package domain

import "testing"

// The pre-centralization middleware (30eb724^) allowed these office/plan pairs.
// The agent-only migration fixtures did not cover the middleware's office rules.
func TestMedicalMigrationPreservesOfficeAcceptance(t *testing.T) {
	for _, tc := range []struct{ office, plan, carrier string }{
		{"Crystal River", "Aetna QHP Individual Exchange", "car40887"},
		{"Crystal River", "AvMed", "car40890"},
		{"Crystal River", "Imagine Health", "car308142"},
		{"Crystal River", "Meritain Health", "car301578"},
		{"Crystal River", "Multiplan PHCS", "car301648"},
		{"Crystal River", "Oscar Health", "car284233"},
		{"Crystal River", "Oscar", "car284233"},
		{"Crystal River", "Oscar Insurance", "car284233"},
		{"Crystal River", "Oscar Health Plans", "car284233"},
		{"Crystal River", "SunHealth", "car308086"},
		{"Crystal River", "Tricare Forever", "car40921"},
		{"Crystal River", "Tricare Prime", "car284327"},
		{"Crystal River", "Tricare Select", "car284327"},
		{"Spring Hill", "Childrens Medical Services", "car281245"},
	} {
		t.Run(tc.office+"/"+tc.plan, func(t *testing.T) {
			office, _ := ResolveOffice(tc.office)
			d := DecideInsurance(tc.plan, "medical", office, "01/02/1980")
			if d.Outcome != "accepted" || d.Participation != "accepted" || !d.CanSchedule || d.CarrierID != tc.carrier {
				t.Fatalf("legacy office acceptance lost: %+v", d)
			}
			chart := PatientDemographics{CarrierID: tc.carrier, CarrierName: d.CanonicalPlan}
			if chartDecision := DecideChartInsurance(chart, d.CanonicalPlan, "medical", office, "01/02/1980"); !chartDecision.CanSchedule {
				t.Fatalf("restored plan cannot survive chart read: %+v", chartDecision)
			}
		})
	}
}

func TestMedicalMigrationPreservesAliases(t *testing.T) {
	for _, tc := range []struct{ alias, plan string }{
		{"community care", "Community Care Plan"},
		{"duo complete", "United Healthcare Dual Complete"},
		{"envolve", "Envolve Vision"},
		{"eye care health", "Eye Care Health Solutions"},
		{"imagine", "Imagine Health"},
		{"sun health", "SunHealth"},
		{"aetna better health medicaid mma (medical)", "Aetna Better Health"},
		{"cigna miami dade", "Cigna Miami Dade Public Schools"},
		{"miami dade public schools", "Cigna Miami Dade Public Schools"},
		{"partners direct health (medical) imagine health", "Partners Direct Health"},
		{"us health group - a unitedhealthcare company", "US Health Group"},
	} {
		for _, name := range []string{"Spring Hill", "Crystal River", "Hollywood", "Sweetwater"} {
			t.Run(name+"/"+tc.alias, func(t *testing.T) {
				office, _ := ResolveOffice(name)
				want := DecideInsurance(tc.plan, "medical", office, "01/02/1980")
				got := DecideInsurance(tc.alias, "medical", office, "01/02/1980")
				if got.Outcome != want.Outcome || got.Participation != want.Participation || got.CanonicalPlan != want.CanonicalPlan || got.CarrierID != want.CarrierID || got.CanSchedule != want.CanSchedule {
					t.Fatalf("alias lost office policy: got %+v; want %+v", got, want)
				}
			})
		}
	}
}
