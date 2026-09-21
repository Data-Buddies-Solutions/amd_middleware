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
				if d.Participation == "accepted" || d.CanSchedule {
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
	for _, plan := range []string{"Cigna HMO or Cigna PPO", "Aetna or Cigna PPO"} {
		t.Run(plan, func(t *testing.T) {
			d := DecideInsurance(plan, "medical", office, "01/02/1980")
			if d.Outcome != "needs_clarification" || (d.Participation == "accepted") || d.CanSchedule {
				t.Fatalf("ambiguous plan selected: %+v", d)
			}
		})
	}
}

func TestMedicalChartProductPreservesExplicitRestrictions(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, label := range []string{"AETNA HMO - ACTIVE", "Aetna HMO or Aetna Commercial", "Active Aetna HMO or Aetna Commercial"} {
		t.Run(label, func(t *testing.T) {
			chart := PatientDemographics{CarrierID: "car40887", CarrierName: label}
			got := DecideChartInsurance(chart, "Aetna Commercial", "medical", office, "01/02/1980")
			if got.CanSchedule {
				t.Fatalf("explicit restricted or ambiguous chart product overridden: %+v", got)
			}
		})
	}
}

func TestMedicalChartProductStillAcceptsConfirmedDirectoryProduct(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, label := range []string{"AETNA", "AETNA INSURANCE", "", "Renamed carrier directory label"} {
		t.Run(label, func(t *testing.T) {
			chart := PatientDemographics{CarrierID: "car40887", CarrierName: label}
			got := DecideChartInsurance(chart, "Aetna Commercial", "medical", office, "01/02/1980")
			if !got.CanSchedule || got.CanonicalPlan != "Aetna Commercial" {
				t.Fatalf("confirmed product lost: %+v", got)
			}
			got = DecideChartInsurance(chart, "Aetna HMO", "medical", office, "01/02/1980")
			if got.CanSchedule || len(got.Requirements) == 0 {
				t.Fatalf("confirmed restriction lost: %+v", got)
			}
			chart.CarrierID = "car999"
			if got = DecideChartInsurance(chart, "Aetna Commercial", "medical", office, "01/02/1980"); got.CanSchedule {
				t.Fatalf("changed carrier accepted: %+v", got)
			}
		})
	}
}

func TestMedicalChartProductKeepsOfficeExclusions(t *testing.T) {
	office, _ := ResolveOffice("Spring Hill")
	chart := PatientDemographics{CarrierID: "car40887", CarrierName: "Aetna EPO - Active"}
	got := DecideChartInsurance(chart, "Aetna Commercial", "medical", office, "01/02/1980")
	if got.CanSchedule || got.Outcome != "not_accepted" {
		t.Fatalf("explicit office exclusion lost: %+v", got)
	}
	chart.CarrierName = "Aetna Commercial PPO - Active"
	got = DecideChartInsurance(chart, "Aetna Commercial PPO", "medical", office, "01/02/1980")
	if !got.CanSchedule || got.CanonicalPlan != "Aetna Commercial PPO" {
		t.Fatalf("matching explicit product blocked: %+v", got)
	}
}

func TestAcceptedMedicalFamilyStillNeedsProductForScheduling(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, plan := range []string{"Aetna", "United Healthcare", "Humana", "Tricare"} {
		accepted := DecideInsurance(plan, "medical", office, "01/02/1980")
		if accepted.Participation != "accepted" {
			t.Fatalf("participation changed for %s: %+v", plan, accepted)
		}
		chart := PatientDemographics{CarrierID: accepted.CarrierID, CarrierName: medicalCatalog.CarrierNames[accepted.CarrierID]}
		got := DecideChartInsurance(chart, accepted.CanonicalPlan, "medical", office, "01/02/1980")
		if got.CanSchedule || got.Outcome != "needs_clarification" {
			t.Errorf("generic %s treated as exact product: %+v", plan, got)
		}
	}
}

func TestMedicalChartFamilyAliasesAcceptConfirmedProduct(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, label := range []string{"UHC", "United Health Care - Active", "United Healthcare NHP"} {
		chart := PatientDemographics{CarrierID: "car40923", CarrierName: label}
		got := DecideChartInsurance(chart, "United Healthcare NHP HMO Access", "medical", office, "01/02/1980")
		if !got.CanSchedule || got.CanonicalPlan != "United Healthcare NHP HMO Access" {
			t.Errorf("family %q discarded confirmed product: %+v", label, got)
		}
		got = DecideChartInsurance(chart, "United Healthcare HMO", "medical", office, "01/02/1980")
		if got.CanSchedule || len(got.Requirements) == 0 {
			t.Errorf("family %q discarded confirmed restrictions: %+v", label, got)
		}
	}
	for _, label := range []string{"UHC or Aetna HMO", "United Healthcare HMO - Active", "United Healthcare NHP HMO Only"} {
		chart := PatientDemographics{CarrierID: "car40923", CarrierName: label}
		if got := DecideChartInsurance(chart, "United Healthcare NHP HMO Access", "medical", office, "01/02/1980"); got.CanSchedule {
			t.Errorf("explicit or ambiguous chart %q overridden: %+v", label, got)
		}
	}
}
