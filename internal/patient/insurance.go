package patient

import (
	"context"
	"strings"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
)

func (p *patient) UpdateInsurance(ctx context.Context, command UpdateInsuranceCommand) (result UpdateInsuranceResult) {
	defer func() {
		recordMutation("update_insurance", updateInsuranceOutcome(result))
	}()

	if domain.IsSelfPayInsurance(command.Insurance) && strings.TrimSpace(command.SubscriberNum) == "" {
		command.SubscriberNum = "self pay"
	}
	if command.PatientID == "" || command.Insurance == "" || command.SubscriberNum == "" {
		return UpdateInsuranceResult{
			Status:  UpdateInsuranceStatusError,
			Outcome: MutationValidationFailed,
			Message: "patientId, insurance, and subscriberNum are required",
		}
	}
	if err := domain.ValidateOptionalDOB(command.DOB); err != nil {
		return UpdateInsuranceResult{
			Status:  UpdateInsuranceStatusError,
			Outcome: MutationValidationFailed,
			Message: err.Error(),
		}
	}
	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: MutationValidationFailed, Message: err.Error()}
	}
	coverage := command.CoverageType
	if coverage == "" {
		coverage = "medical"
	}
	decision := domain.DecideInsurance(command.Insurance, coverage, office, command.DOB)
	if decision.Participation != "accepted" {
		return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: MutationValidationFailed, Message: decision.Answer}
	}

	reconciled, replacementAlreadyActive, outcome := p.endInsurance(ctx, command, decision.CarrierID)
	if outcome != "" {
		return updateInsuranceFailure(outcome, "Failed to update existing insurance in AdvancedMD. Please try again or contact the office.")
	}

	if !replacementAlreadyActive {
		addReconciled, outcome := p.addInsurance(ctx, domain.PatientInsurance{
			PatientID:     command.PatientID,
			RespPartyID:   command.RespPartyID,
			CarrierID:     decision.CarrierID,
			SubscriberNum: command.SubscriberNum,
		})
		reconciled = reconciled || addReconciled
		if outcome != "" {
			return updateInsuranceFailure(outcome, "Failed to attach new insurance in AdvancedMD. Please try again or contact the office.")
		}
	}

	result = UpdateInsuranceResult{
		Status:            UpdateInsuranceStatusUpdated,
		PatientID:         command.PatientID,
		OldInsurance:      command.OldInsurance,
		NewInsurance:      command.Insurance,
		Routing:           decision.Routing,
		AllowedProviders:  decision.AllowedProviders,
		RoutingAmbiguous:  decision.Participation == "unknown",
		PreauthRequired:   len(decision.Requirements) > 0,
		InsuranceDecision: &decision,
		Message:           "Insurance updated successfully",
	}
	if reconciled {
		result.Outcome = MutationReconciledSuccess
	}
	return result
}

func updateInsuranceFailure(outcome MutationOutcome, message string) UpdateInsuranceResult {
	switch outcome {
	case MutationRejected:
		message = "AdvancedMD rejected the insurance update. Please contact the office."
	case MutationReconciledFailure:
		message = "AdvancedMD did not apply the insurance update. Please try again or contact the office."
	case MutationIndeterminateWrite:
		message = "The insurance update may have been applied, but the outcome could not be confirmed. Do not retry automatically; contact the office."
	}
	return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: outcome, Message: message}
}

func (p *patient) endInsurance(ctx context.Context, command UpdateInsuranceCommand, replacementCarrierID string) (bool, bool, MutationOutcome) {
	if command.InsPlanID == "" {
		return false, false, ""
	}
	err := p.advancedMD.EndDatePatientInsurance(ctx, domain.PatientInsuranceEnd{
		PatientID: command.PatientID,
		InsPlanID: command.InsPlanID,
	})
	if err == nil {
		return false, false, ""
	}
	switch advancedmd.MutationFailureOf(err) {
	case advancedmd.MutationRejected:
		return false, false, MutationRejected
	case advancedmd.MutationAmbiguous:
		demographics, known := p.reconcileInsurance(ctx, command.PatientID)
		if !known {
			return false, false, MutationIndeterminateWrite
		}
		if demographics.InsPlanID == command.InsPlanID {
			return false, false, MutationReconciledFailure
		}
		replacement := domain.PatientInsurance{
			RespPartyID:   command.RespPartyID,
			CarrierID:     replacementCarrierID,
			SubscriberNum: command.SubscriberNum,
		}
		return true, insuranceMatches(demographics, replacement), ""
	default:
		return false, false, failureOutcome(err)
	}
}

func (p *patient) addInsurance(ctx context.Context, command domain.PatientInsurance) (bool, MutationOutcome) {
	err := p.advancedMD.AddPatientInsurance(ctx, command)
	if err == nil {
		return false, ""
	}
	switch advancedmd.MutationFailureOf(err) {
	case advancedmd.MutationRejected:
		return false, MutationRejected
	case advancedmd.MutationAmbiguous:
		demographics, known := p.reconcileInsurance(ctx, command.PatientID)
		if !known {
			return false, MutationIndeterminateWrite
		}
		if !insuranceMatches(demographics, command) {
			return false, MutationReconciledFailure
		}
		return true, ""
	default:
		return false, failureOutcome(err)
	}
}

func insuranceMatches(demographics domain.PatientDemographics, intended domain.PatientInsurance) bool {
	return demographics.CarrierID == intended.CarrierID &&
		demographics.RespPartyID == intended.RespPartyID &&
		strings.EqualFold(strings.TrimSpace(demographics.SubscriberNum), strings.TrimSpace(intended.SubscriberNum))
}

func (p *patient) reconcileInsurance(ctx context.Context, patientID string) (domain.PatientDemographics, bool) {
	demographics, err := retryRead(ctx, func() (domain.PatientDemographics, error) {
		return p.advancedMD.GetPatientDemographics(ctx, patientID)
	})
	if err != nil || !demographics.InsuranceStateKnown {
		return domain.PatientDemographics{}, false
	}
	return demographics, true
}
