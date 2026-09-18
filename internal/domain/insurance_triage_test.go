package domain

import (
	"testing"
)

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

func TestLegacyAuthorizationIsOfficeScoped(t *testing.T) {
	for _, name := range []string{"Hollywood", "Sweetwater", "Spring Hill", "Crystal River"} {
		office, _ := ResolveOffice(name)
		d := DecideInsurance("Aetna HMO", "medical", office, "01/02/1980")
		needsAuth := name == "Hollywood" || name == "Sweetwater"
		if needsAuth && (d.Outcome != "needs_staff_task" || d.CanSchedule || len(d.Requirements) == 0) {
			t.Fatalf("lost old authorization at %s: %+v", name, d)
		}
		if !needsAuth && len(d.Requirements) > 0 {
			t.Fatalf("added authorization at %s: %+v", name, d)
		}
	}
}
