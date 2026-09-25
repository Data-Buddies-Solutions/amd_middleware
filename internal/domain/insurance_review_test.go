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
