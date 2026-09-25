package domain

import "testing"

func TestMedicalDocumentCarrierAttachments(t *testing.T) {
	office, _ := ResolveOffice("Sweetwater")
	for _, tc := range []struct {
		plan, code, id string
		schedule       bool
	}{
		{"Humana Medicaid HMO", "HUM02", "car303033", false},
		{"Cigna PPO", "CIG09", "car40895", true},
		{"Cigna Open Access", "CIG09", "car40895", true},
		{"Cigna Miami Dade Public Schools", "CIG09", "car40895", true},
		{"Humana Medicare PPO", "HUM PPO", "car303062", true},
		{"Humana Premier HMO", "HUMPHMO", "car303061", true},
		{"Molina Medicare", "MOLI2", "car301507", true},
		{"Molina Medicaid", "ICA01", "car40907", true},
		{"UMR", "UNIT3", "car284838", true},
		{"US Health Group", "UNIT3", "car284838", true},
		{"Tricare Prime", "TRI00", "car284327", false},
		{"Tricare Select", "TRI00", "car284327", true},
		{"Tricare For Life", "TRI05", "car40921", true},
		{"Straight Medicaid", "FLO03", "car40899", true},
		{"Aetna HMO", "AET07", "car40887", false},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			d := DecideInsurance(tc.plan, "medical", office, "01/02/1980")
			if d.CarrierCode != tc.code || d.CarrierID != tc.id || d.Participation != "accepted" || d.CanSchedule != tc.schedule {
				t.Fatalf("wrong attachment or permission: %+v", d)
			}
			// An old/wrong chart attachment must never become schedulable through caller text.
			chart := PatientDemographics{CarrierName: tc.plan, CarrierID: "car308175"}
			if DecideChartInsurance(chart, tc.plan, "medical", office, "01/02/1980").CanSchedule {
				t.Fatal("stale carrier attachment allowed scheduling")
			}
		})
	}
}
