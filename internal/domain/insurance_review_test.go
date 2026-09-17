package domain

import "testing"

func TestChartProductCannotLoseRequirementsThroughSharedCarrier(t *testing.T) {
	InitRegistry("")
	office, _ := ResolveOffice("Hollywood")
	for _, tc := range []struct{ chart, requested, carrier string }{
		{"Cigna HMO", "Cigna PPO", "car301345"},
		{"United Healthcare NHP HMO Only", "United Healthcare NHP HMO Access", "car40923"},
	} {
		t.Run(tc.chart, func(t *testing.T) {
			d := DecideChartInsurance(PatientDemographics{CarrierName: tc.chart, CarrierID: tc.carrier}, tc.requested, "medical", office, "01/02/1980")
			if d.CanSchedule {
				t.Fatalf("caller removed chart product requirements: %+v", d)
			}
		})
	}
}

func TestChartProductAcceptsCanonicalAliasesButNotUnknownCarrier(t *testing.T) {
	InitRegistry("")
	office, _ := ResolveOffice("Hollywood")
	chart := PatientDemographics{CarrierName: "Preferred Care Partners Medical", CarrierID: "car40916"}
	d := DecideChartInsurance(chart, "Preferred Care Partners", "medical", office, "01/02/1980")
	if !d.CanSchedule || d.CarrierCode != "PRE04" {
		t.Fatalf("same product alias blocked: %+v", d)
	}
	chart.CarrierID = "car999999"
	d = DecideChartInsurance(chart, "Preferred Care Partners", "medical", office, "01/02/1980")
	if d.CanSchedule {
		t.Fatalf("unknown chart carrier permitted: %+v", d)
	}
}

func TestPRE04CorrectionPreservesOfficeParticipation(t *testing.T) {
	InitRegistry("")
	for _, name := range []string{"Spring Hill", "Hollywood", "Sweetwater"} {
		t.Run(name, func(t *testing.T) {
			office, _ := ResolveOffice(name)
			d := DecideInsurance("Preferred Care Partners", "medical", office, "01/02/1980")
			if name == "Spring Hill" {
				if d.CanRegister || d.CanSchedule || d.Participation == "accepted" {
					t.Fatalf("correction expanded office participation: %+v", d)
				}
			} else if !d.CanSchedule || d.CarrierCode != "PRE04" {
				t.Fatalf("participating office blocked: %+v", d)
			}
		})
	}
}

func TestInsuranceClarifiesAmbiguousNaturalLanguage(t *testing.T) {
	InitRegistry("")
	office, _ := ResolveOffice("Hollywood")
	for _, plan := range []string{"I have Humana", "Humana Unknown Product", "I have United Healthcare", "Cigna HMO or Cigna PPO", "Aetna or Cigna PPO"} {
		t.Run(plan, func(t *testing.T) {
			d := DecideInsurance(plan, "medical", office, "01/02/1980")
			if d.Outcome != "needs_clarification" || d.CanRegister || d.CanSchedule {
				t.Fatalf("ambiguous plan selected: %+v", d)
			}
		})
	}
}
