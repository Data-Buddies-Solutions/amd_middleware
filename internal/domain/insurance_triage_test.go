package domain

import (
	"strings"
	"testing"
)

func TestMedicalTriageAsksForProductBeforeChoosingCarrier(t *testing.T) {
	for _, officeName := range []string{"Hollywood", "Sweetwater", "Spring Hill", "Crystal River"} {
		office, _ := ResolveOffice(officeName)
		for _, tc := range []struct{ input, question string }{
			{"Humana", "Medicare, Medicaid"}, {"I have Humana", "Medicare, Medicaid"},
			{"UHC", "individual/exchange"}, {"UnitedHealthcare", "individual/exchange"},
			{"UHC Medicare", "AARP Medicare Complete or Dual Complete"},
			{"United Healthcare Medicare Advantage", "AARP Medicare Complete or Dual Complete"},
			{"United Health Care Medicare", "AARP Medicare Complete or Dual Complete"},
			{"Molina", "Medicaid, Medicare, or Marketplace"},
			{"Cigna", "HMO, PPO, or Open Access"},
			{"Aetna", "commercial/employer"}, {"Tricare", "Prime, Select, or For Life"},
		} {
			t.Run(officeName+"/"+tc.input, func(t *testing.T) {
				d := DecideInsurance(tc.input, "medical", office, "")
				if d.Outcome != "needs_clarification" || d.CanonicalPlan != "" || d.CarrierID != "" || d.CanRegister || d.CanSchedule || !strings.Contains(d.Answer, tc.question) {
					t.Fatalf("ambiguous insurer did not produce actionable triage: %+v", d)
				}
			})
		}
	}
}

func TestMedicalTriageRecognizesClarifiedAetnaCommercial(t *testing.T) {
	for _, name := range []string{"Hollywood", "Spring Hill"} {
		office, _ := ResolveOffice(name)
		d := DecideInsurance("Aetna Commercial", "medical", office, "01/01/2015")
		if !d.CanRegister || !d.CanSchedule || d.CarrierCode != "AET07" {
			t.Fatalf("clarified product blocked: %+v", d)
		}
	}
}

func TestMedicalTriageRefinesHumanaWithoutLosingProductRequirements(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, q := range []string{"Humana Medicare", "Humana HMO"} {
		d := DecideInsurance(q, "medical", office, "")
		if d.CanRegister || d.CanSchedule || !strings.Contains(d.Answer, "HMO") || !strings.Contains(d.Answer, "plan name") {
			t.Fatalf("missing next question: %+v", d)
		}
	}
	for _, tc := range []struct {
		plan, code string
		schedule   bool
	}{
		{"Humana Medicare PPO", "HUM PPO", true},
		{"Humana Medicare HMO", "ICA01", true},
		{"Humana Medicaid HMO", "HUM02", false},
		{"Humana Premier HMO", "HUMPHMO", true},
		{"Cigna PPO", "CIG09", true},
		{"Cigna HMO", "CIGN1", false},
		{"Tricare Select", "TRI00", true},
		{"Tricare Prime", "TRI00", false},
	} {
		d := DecideInsurance(tc.plan, "medical", office, "")
		if !d.CanRegister || d.CarrierCode != tc.code || d.CanSchedule != tc.schedule {
			t.Errorf("specific product lost: %s %+v", tc.plan, d)
		}
	}
	aarp := DecideInsurance("United AARP Medicare Complete", "medical", office, "")
	if aarp.CanonicalPlan != "United Healthcare AARP Medicare" || aarp.CarrierCode != "AARPM" {
		t.Fatalf("explicit AARP no longer recognized: %+v", aarp)
	}
}
