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

func TestSimplyTriagePreservesProductRequirementsAndOfficeScope(t *testing.T) {
	for _, officeName := range []string{"Hollywood", "Sweetwater", "Spring Hill", "Crystal River"} {
		office, _ := ResolveOffice(officeName)
		for _, plan := range []string{"Simply", "Simply Health", "Simply Healthcare", "Simply Health Plans"} {
			d := DecideInsurance(plan, "medical", office, "01/02/1980")
			if d.CanRegister || d.CanSchedule || d.CarrierID != "" {
				t.Errorf("%s/%s guessed a product: %+v", officeName, plan, d)
			}
			if officeName == "Crystal River" {
				if d.Participation != "not_accepted" {
					t.Errorf("%s/%s lost office exclusion: %+v", officeName, plan, d)
				}
			} else if d.Outcome != "needs_clarification" || !strings.Contains(d.Answer, "Medicaid or Medicare") {
				t.Errorf("%s/%s missing product question: %+v", officeName, plan, d)
			}
		}
		for _, plan := range []string{"Simply Medicaid", "Simply Medicare"} {
			d := DecideInsurance(plan, "medical", office, "01/02/1980")
			if d.CanSchedule {
				t.Errorf("%s/%s lost insurance hold: %+v", officeName, plan, d)
			}
			participates := officeName == "Hollywood" || officeName == "Sweetwater" || (officeName == "Spring Hill" && plan == "Simply Medicaid")
			if participates && (!d.CanRegister || d.CarrierCode != "ICA01" || len(d.Requirements) != 1 || d.Requirements[0].Kind != "precertification") {
				t.Errorf("%s/%s lost explicit product policy: %+v", officeName, plan, d)
			}
			if !participates && d.CanRegister {
				t.Errorf("%s/%s expanded office participation: %+v", officeName, plan, d)
			}
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
