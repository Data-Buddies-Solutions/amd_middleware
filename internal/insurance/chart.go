package insurance

import "advancedmd-token-management/internal/domain"

func DecideChartInsurance(chart domain.PatientDemographics, coverage string, office *domain.OfficeConfig, dob string) InsuranceDecision {
	d, list, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	var plans []plan
	for _, p := range list.Plans {
		if p.Coverage == coverage && chart.CarrierID != "" && p.CarrierID == chart.CarrierID {
			plans = append(plans, p)
		}
	}
	groups := distinctOutcomes(plans, office, dob)
	if len(groups) != 1 || groups[0].Participation != "accepted" {
		d.Outcome = "needs_staff_task"
		d.Reason, d.Answer = "chart_unverified", answerChartStaff
		return d
	}
	return groups[0]
}
