package insurance

import (
	"slices"
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
			d := DecideChartInsurance(chart, "medical", hollywood, adultDOB, testNow)
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
	d := DecideChartInsurance(chart, "medical", office(t, "spring_hill"), adultDOB, testNow)
	if d.PlanID != "prior-auth-plan" || d.Participation != "accepted" || d.CanSchedule || d.Answer != answerPriorAuth {
		t.Fatalf("decision = %+v", d)
	}
}

func TestNotePlanIDsNameThePlanTheStaffNoteNames(t *testing.T) {
	hollywood := office(t, "hollywood")
	simplyMedicaid := []string{"simply-medicaid-medical", "simply-medicaid-vision"}
	tests := []struct {
		name      string
		note      string
		carrierID string
		want      []string
	}{
		{"plan behind an administrator", "Simply Medicaid Vision:Icare", "car40907", simplyMedicaid},
		{"note with codes around the plan", "simply medicaid SEQ 0 vision: icare", "car40907", simplyMedicaid},
		{"plan name only", "Community Care Plan", "car40907", []string{"community-care-plan-medical", "community-care-plan-vision"}},
		{"words of a longer name out of order", "Community Care Plan Florida", "car40907", []string{"community-care-plan-medical", "community-care-plan-vision"}},
		{"administrator name beside the plan", "Simply Medicaid:iCare Health Options", "car40907", simplyMedicaid},
		{"administrator name alone", "Icare", "car40907", nil},
		{"one ordinary word that is also an alias", "pt simply wants appt", "car40907", nil},
		{"words split by a period", "Verified Simply. Medicare secondary", "car40907", nil},
		{"words split by a comma", "Simply, Medicare is primary", "car40907", nil},
		{"a plan name after an unrelated sentence", "Pt needs eye care. Health plan: Simply Medicaid", "car40907", simplyMedicaid},
		{"two plans named for vision, one for medical", "Old: Humana Medicare PPO. New: Simply Medicaid", "car40907", []string{"simply-medicaid-medical"}},
		{"a termed plan and a current plan", "Termed Aetna Medicare PPO 12/31, now Simply Medicaid", "car40907", nil},
		{"two plans named equally", "Simply Medicare or Simply Medicaid", "car40907", nil},
		{"plan on another carrier", "Simply Medicaid", "car999", nil},
		{"no carrier on the chart", "Simply Medicaid", "", nil},
		{"no note", "", "car40907", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NotePlanIDs(tc.note, tc.carrierID, hollywood); !slices.Equal(got, tc.want) {
				t.Fatalf("NotePlanIDs(%q) = %v, want %v", tc.note, got, tc.want)
			}
		})
	}
}
