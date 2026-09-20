package patient

import (
	"context"
	"strings"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
)

func (p *patient) UpdateInsurance(ctx context.Context, command UpdateInsuranceCommand) (result UpdateInsuranceResult) {
	oldEnded := false
	defer func() {
		switch {
		case result.Status == UpdateInsuranceStatusUpdated:
			result.Effect = "completed"
		case result.Outcome == MutationIndeterminateWrite:
			result.Effect = "uncertain"
		case oldEnded:
			result.Effect = "partial"
			result.Message = "The old insurance was ended, but the replacement was not attached. Do not retry automatically; contact the office."
		default:
			result.Effect = "no_effect"
		}
		recordMutation("update_insurance", updateInsuranceOutcome(result))
	}()

	if domain.IsSelfPayInsurance(command.Insurance) && strings.TrimSpace(command.SubscriberNum) == "" {
		command.SubscriberNum = "self pay"
	}
	if command.PatientID == "" || command.DOB == "" || command.Insurance == "" || command.SubscriberNum == "" {
		return UpdateInsuranceResult{
			Status:  UpdateInsuranceStatusError,
			Outcome: MutationValidationFailed,
			Message: "patientId, dob, insurance, and subscriberNum are required",
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

	chart, err := retryRead(ctx, func() (domain.PatientDemographics, error) {
		return p.advancedMD.GetPatientDemographics(ctx, command.PatientID)
	})
	if err != nil {
		return updateInsuranceFailure(failureOutcome(err), "Unable to read current insurance. No update was attempted.")
	}
	if chart.DOB == "" || domain.NormalizeDOB(chart.DOB) != domain.NormalizeDOB(command.DOB) {
		return updateInsuranceFailure(MutationValidationFailed, "Patient details changed. Verify the patient again.")
	}
	if !chart.InsuranceStateKnown || chart.RespPartyID == "" ||
		(chart.InsPlanID == "" && (chart.CarrierID != "" || chart.CarrierName != "" || chart.SubscriberNum != "")) {
		return updateInsuranceFailure(MutationValidationFailed, "Current insurance references are incomplete. Contact the office; no update was attempted.")
	}
	// Legacy caller snapshots are accepted but never authorize provider writes.
	command.InsPlanID = chart.InsPlanID
	command.RespPartyID = chart.RespPartyID
	command.OldInsurance = chart.CarrierName
	replacement := domain.PatientInsurance{
		PatientID: command.PatientID, RespPartyID: chart.RespPartyID,
		CarrierID: decision.CarrierID, SubscriberNum: command.SubscriberNum,
	}
	replacementAlreadyActive := chart.InsPlanID != "" && insuranceMatches(chart, replacement)
	reconciled := false
	if !replacementAlreadyActive {
		var outcome MutationOutcome
		reconciled, replacementAlreadyActive, outcome = p.endInsurance(ctx, command, decision.CarrierID)
		if outcome != "" {
			return updateInsuranceFailure(outcome, "Failed to end current insurance. Contact the office.")
		}
		oldEnded = chart.InsPlanID != ""
	}
	if !replacementAlreadyActive {
		addReconciled, outcome := p.addInsurance(ctx, replacement)
		reconciled = reconciled || addReconciled
		if outcome != "" {
			return updateInsuranceFailure(outcome, "Failed to attach new insurance. Contact the office.")
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
		if demographics.InsPlanID != "" && !insuranceMatches(demographics, replacement) {
			return false, false, MutationIndeterminateWrite
		}
		return true, demographics.InsPlanID != "" && insuranceMatches(demographics, replacement), ""
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
		if demographics.InsPlanID != "" && insuranceMatches(demographics, command) {
			return true, ""
		}
		if demographics.InsPlanID != "" {
			return false, MutationIndeterminateWrite
		}
		return false, MutationReconciledFailure
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
	if err != nil || !demographics.InsuranceStateKnown ||
		(demographics.InsPlanID == "" && (demographics.CarrierID != "" || demographics.CarrierName != "" || demographics.SubscriberNum != "")) {
		return domain.PatientDemographics{}, false
	}
	return demographics, true
}
