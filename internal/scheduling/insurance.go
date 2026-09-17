package scheduling

import (
	"advancedmd-token-management/internal/domain"
	"context"
	"strings"
)

func (s *service) insuranceForSearch(ctx context.Context, patientID, plan, coverage string, office *domain.OfficeConfig, dob string) (domain.InsuranceDecision, error) {
	chart, err := s.records.GetPatientDemographics(ctx, patientID)
	if err != nil {
		return domain.InsuranceDecision{}, schedulingError("Unable to verify insurance before scheduling. Ask office staff for help.")
	}
	if domain.NormalizeDOB(chart.DOB) != domain.NormalizeDOB(dob) {
		return domain.InsuranceDecision{}, schedulingError("Patient details changed. Verify the patient again.")
	}
	decision := domain.DecideChartInsurance(chart, plan, coverage, office, dob)
	if !decision.CanSchedule {
		return decision, schedulingError(decision.Answer)
	}
	return decision, nil
}

// A hospital follow-up is still a medical visit with insurance requirements.
func validateHospitalFollowUp(reason, hospital, date string) error {
	reason = strings.ToLower(reason)
	if strings.Contains(reason, "hospital") {
		missing := []string{}
		if strings.TrimSpace(hospital) == "" {
			missing = append(missing, "hospitalName")
		}
		if strings.TrimSpace(date) == "" {
			missing = append(missing, "hospitalDate")
		}
		if len(missing) > 0 {
			return &Error{category: CategoryValidation, message: "Ask which hospital and when the hospital visit occurred before scheduling the follow-up.", missing: missing}
		}
	}
	return nil
}
