package scheduling

import (
	"advancedmd-token-management/internal/domain"
	"context"
)

func (s *service) insuranceForSearch(ctx context.Context, patientID, plan, coverage string, office *domain.OfficeConfig, dob string) (domain.InsuranceDecision, error) {
	chart, err := s.records.GetPatientDemographics(ctx, patientID)
	if err != nil {
		return domain.InsuranceDecision{}, providerError(err, "Unable to verify insurance before scheduling. Retry once; if it still fails, contact office staff.")
	}
	if chart.DOB == "" || dob == "" || domain.NormalizeDOB(chart.DOB) != domain.NormalizeDOB(dob) {
		return domain.InsuranceDecision{}, schedulingError("Patient details changed. Verify the patient again.")
	}
	decision := domain.DecideChartInsurance(chart, plan, coverage, office, dob)
	if !decision.CanSchedule {
		return decision, categorizedError(CategoryPolicyBlocked, decision.Answer)
	}
	return decision, nil
}
