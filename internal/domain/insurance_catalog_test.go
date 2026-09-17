package domain

import "testing"

func TestMedicalCatalogPreservesOfficeExclusions(t *testing.T) {
	for _, tc := range []struct{ office, plan string }{
		{"Spring Hill", "Florida Blue Select"}, {"Spring Hill", "Florida Blue BlueSelect"},
		{"Spring Hill", "Care Plus"}, {"Spring Hill", "Humana Gold Plus"},
		{"Spring Hill", "Aetna EPO"}, {"Spring Hill", "Preferred Care Partners"},
		{"Crystal River", "Ambetter"}, {"Crystal River", "Molina Medicaid"},
		{"Hollywood", "Molina Marketplace"}, {"Hollywood", "Cigna Local Plus"},
	} {
		office, _ := ResolveOffice(tc.office)
		d := DecideInsurance(tc.plan, "medical", office, "")
		if d.Outcome != "not_accepted" || d.CanRegister || d.CanSchedule {
			t.Errorf("office exclusion lost: %+v => %+v", tc, d)
		}
	}
}

func TestNamedMedicalProductNeverFallsBackToParent(t *testing.T) {
	office, _ := ResolveOffice("Spring Hill")
	for _, q := range []string{"Devoted Medicare HMO", "Clear Spring Health Medicare Advantage", "Humana Medicare PPO"} {
		d := DecideInsurance(q, "medical", office, "")
		if d.Outcome != "needs_staff_task" || d.CanRegister || d.CanSchedule || d.CarrierID != "" {
			t.Errorf("specific product inherited a parent mapping: %s %+v", q, d)
		}
	}
}

func TestSharedMedicalAliasesUseThePlanIdentity(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, tc := range []struct{ input, code string }{
		{"Florida Blue PPO", "FLO01"}, {"BCBS PPO", "FLO01"},
		{"Meritain Health - Aetna", "MERI1"}, {"Sunshine Health", "AMBE1"},
		{"Florida Complete Care - Medicare Medical ONLY", "ICA01"},
		{"Aetna Commercial", "AET07"}, {"Aetna Medicare PPO", "ICA01"},
		{"Self-pay", "SELF"}, {"UMR (United Health One)", "UNIT3"},
	} {
		d := DecideInsurance(tc.input, "medical", office, "")
		if !d.CanRegister || d.CarrierCode != tc.code {
			t.Errorf("alias lost identity: %s %+v", tc.input, d)
		}
	}
}

func TestMedicalAliasesHaveOneOwner(t *testing.T) {
	owners := map[string]string{}
	for _, p := range medicalPlans {
		for _, alias := range append([]string{p.Name}, p.Aliases...) {
			key := insuranceNormalize(alias)
			if owner, ok := owners[key]; ok && owner != p.Name {
				t.Errorf("alias %q belongs to both %s and %s", alias, owner, p.Name)
			}
			owners[key] = p.Name
		}
	}
}
