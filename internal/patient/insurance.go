package patient

import (
	"context"
	"fmt"
	"strings"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
)

type insuranceSelection struct {
	entry  domain.InsuranceEntry
	mode   domain.InsuranceMode
	policy domain.SchedulingPolicy
}

func selectInsurance(name, coverageType string, office *domain.OfficeConfig) (insuranceSelection, string) {
	mode := domain.InsuranceModeForCoverage(coverageType)
	entry, ok := domain.LookupInsuranceForCoverageAtOffice(name, mode, office)
	policy := domain.NewSchedulingPolicy(office)

	switch {
	case mode == domain.InsuranceModeVision && !policy.SupportsRouting(domain.RoutingOpticalOnly):
		return insuranceSelection{}, fmt.Sprintf("Routine vision coverage is not supported at %s. Route the patient to Spring Hill routine vision scheduling.", office.DisplayName)
	case mode == domain.InsuranceModeMedical && !policy.SupportsMedical():
		return insuranceSelection{}, fmt.Sprintf("Medical coverage is not supported at %s. Use routine vision coverage for this office or route medical visits to a medical office.", office.DisplayName)
	case !ok:
		return insuranceSelection{}, fmt.Sprintf("Insurance not recognized: %q. Please use an insurance name from the accepted list.", name)
	case entry.Routing == domain.RoutingNotAccepted:
		return insuranceSelection{}, fmt.Sprintf("%s is not accepted at %s.", name, office.DisplayName)
	default:
		return insuranceSelection{entry: entry, mode: mode, policy: policy}, ""
	}
}

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
	office, err := p.offices.ResolveOffice(command.Office)
	if err != nil {
		return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: MutationValidationFailed, Message: err.Error()}
	}
	selection, message := selectInsurance(command.Insurance, command.CoverageType, office)
	if message != "" {
		return UpdateInsuranceResult{Status: UpdateInsuranceStatusError, Outcome: MutationValidationFailed, Message: message}
	}

	reconciled, replacementAlreadyActive, outcome := p.endInsurance(ctx, command, selection.entry.CarrierID)
	if outcome != "" {
		return updateInsuranceFailure(outcome, "Failed to update existing insurance in AdvancedMD. Please try again or contact the office.")
	}

	if !replacementAlreadyActive {
		addReconciled, outcome := p.addInsurance(ctx, domain.PatientInsurance{
			PatientID:     command.PatientID,
			RespPartyID:   command.RespPartyID,
			CarrierID:     selection.entry.CarrierID,
			SubscriberNum: command.SubscriberNum,
		})
		reconciled = reconciled || addReconciled
		if outcome != "" {
			return updateInsuranceFailure(outcome, "Failed to attach new insurance in AdvancedMD. Please try again or contact the office.")
		}
	}

	routing := selection.policy.SchedulingRouting(selection.entry.Routing, command.DOB)
	_, ambiguous := domain.RoutingForDemographicInsurance(selection.entry.CarrierID, command.Insurance, office)
	result = UpdateInsuranceResult{
		Status:           UpdateInsuranceStatusUpdated,
		PatientID:        command.PatientID,
		OldInsurance:     command.OldInsurance,
		NewInsurance:     command.Insurance,
		Routing:          routing,
		AllowedProviders: selection.policy.ProviderNames(routing, command.DOB),
		RoutingAmbiguous: ambiguous,
		PreauthRequired:  selection.entry.PreauthRequired,
		Message:          "Insurance updated successfully",
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
