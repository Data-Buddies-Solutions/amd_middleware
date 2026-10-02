package insurance

import (
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestDecideChartInsuranceUsesTheChartCarrier(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	hollywood := office(t, "hollywood")
	tests := []struct {
		name      string
		carrierID string
		outcome   string
		planID    string
	}{
		{"one outcome for the carrier", "car40907", "accepted", "aetna-better-health"},
		{"carrier plans end differently", "car40887", "needs_staff_task", ""},
		{"carrier not in the list", "car999", "needs_staff_task", ""},
		{"no carrier on the chart", "", "needs_staff_task", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chart := domain.PatientDemographics{CarrierID: tc.carrierID, CarrierName: "DIRECTORY NAME"}
			d := DecideChartInsurance(chart, "medical", hollywood, adultDOB)
			if d.Outcome != tc.outcome || d.PlanID != tc.planID {
				t.Fatalf("decision = %+v", d)
			}
			if tc.outcome == "needs_staff_task" && (d.CanSchedule || d.Answer != answerChartStaff) {
				t.Fatalf("staff decision = %+v", d)
			}
		})
	}
}

func TestDecideChartInsuranceKeepsRequirementDecisions(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), springHillPlans(), nil)
	chart := domain.PatientDemographics{CarrierID: "car4"}
	d := DecideChartInsurance(chart, "medical", office(t, "spring_hill"), adultDOB)
	if d.PlanID != "prior-auth-plan" || d.Participation != "accepted" || d.CanSchedule || d.Answer != answerPriorAuth {
		t.Fatalf("decision = %+v", d)
	}
}
