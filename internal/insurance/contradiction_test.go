package insurance

import "testing"

func contradictionPlans() []plan {
	farnan := map[string]string{"Dr. Kyler Farnan": "yes"}
	return []plan{
		{ID: "staywell-medicare", Label: "Staywell Medicare", Coverage: "medical", Names: []string{"Staywell"}, CarrierID: "car1", Doctors: bach("yes")},
		{ID: "sunshine-medicaid", Label: "Sunshine Medicaid", Coverage: "medical", Names: []string{"Sunshine", "Sunshine Health"}, CarrierID: "car2", Doctors: bach("yes")},
		{ID: "oscar-health", Label: "Oscar Health", Coverage: "medical", Names: []string{"Oscar"}, CarrierID: "car3", Doctors: bach("yes")},
		{ID: "humana-medicare", Label: "Humana Medicare", Coverage: "medical", CarrierID: "car4", Doctors: bach("yes")},
		{ID: "tricare-select", Label: "Tricare Select", Coverage: "medical", CarrierID: "car5", Doctors: bach("yes")},
		{ID: "tricare-humana-military", Label: "Tricare Humana Military", Coverage: "medical", CarrierID: "car1", Doctors: bach("no")},
		{ID: "cigna-vision", Label: "Cigna Vision", Coverage: "routine_vision", Names: []string{"Cigna"}, CarrierID: "car1", Doctors: farnan},
		{ID: "united-vision", Label: "UnitedHealthcare Vision", Coverage: "routine_vision", Names: []string{"United Healthcare"}, CarrierID: "car2", Doctors: farnan},
		{ID: "humana-medicare-vision", Label: "Humana Medicare Vision", Coverage: "routine_vision", CarrierID: "car3", Doctors: farnan},
		{ID: "ambetter-vision", Label: "Ambetter Vision", Coverage: "routine_vision", Names: []string{"Ambetter"}, CarrierID: "car4", Doctors: farnan},
		{ID: "premier-eye-care", Label: "Premier Eye Care", Coverage: "routine_vision", Names: []string{"Premier"}, CarrierID: "car5", Doctors: farnan},
	}
}

func TestDecideInsuranceNeverPicksAPlanTheCallerContradicted(t *testing.T) {
	useSyntheticCatalog(t, contradictionPlans(), nil, nil)
	hollywood := office(t, "hollywood")
	tests := []struct {
		heard, coverage, planID string
	}{
		{"Staywell Medicaid", "medical", ""},
		{"Sunshine Medicare", "medical", ""},
		{"Oscar Medicare", "medical", ""},
		{"Tricare Humana Military (Select)", "medical", "tricare-humana-military"},
		{"United Healthcare Medicare", "routine_vision", ""},
		{"Cigna Medicare Advantage HMO", "routine_vision", ""},
		{"Ambetter Premier", "routine_vision", ""},
		{"Staywell", "medical", "staywell-medicare"},
		{"I have Oscar through my job", "medical", "oscar-health"},
		{"Cigna PPO", "routine_vision", "cigna-vision"},
	}
	for _, tc := range tests {
		d := DecideInsurance(tc.heard, tc.coverage, hollywood, adultDOB)
		if d.PlanID != tc.planID {
			t.Errorf("%q %s: decision = %+v", tc.heard, tc.coverage, d)
		}
	}
}

func TestRealDataNeverPicksAPlanTheCallerContradicted(t *testing.T) {
	hollywood := office(t, "hollywood")
	tests := []struct {
		heard, coverage, contradicted string
	}{
		{"Staywell Medicaid", "medical", "staywell-medicare-medical"},
		{"Sunshine Medicare", "medical", "sunshine-medicaid-medical"},
		{"Oscar Medicare", "medical", "oscar-health-medical"},
		{"Tricare Humana Military (Select)", "medical", "tricare-select"},
		{"United Healthcare Medicare", "routine_vision", "unitedhealthcare-vision"},
		{"Cigna Medicare Advantage HMO", "routine_vision", "cigna-vision"},
		{"Ambetter Premier", "routine_vision", "ambetter-vision"},
		{"Humana Gold PPO", "medical", "humana-medicare-hmo"},
		{"BCBS HMO", "routine_vision", "florida-blue-medicare-vision"},
		{"Molina Medicade", "medical", "molina-medicare-medical"},
		{"Medicare Supplement", "medical", "tricare-for-life"},
	}
	for _, tc := range tests {
		d := DecideInsurance(tc.heard, tc.coverage, hollywood, adultDOB)
		if d.PlanID == tc.contradicted {
			t.Errorf("%q %s resolved to contradicted plan: %+v", tc.heard, tc.coverage, d)
		}
	}
}
