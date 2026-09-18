package domain

import "testing"

func TestParticipationMatchSpecificProducts(t *testing.T) {
	for _, tc := range []struct {
		name, source, query, canonical, status string
	}{
		{"specific accepted", "SPRING_HILL_MEDICAL", "I have Cigna PPO", "Cigna PPO", "accepted"},
		{"specific rejected", "SPRING_HILL_MEDICAL", "I have Cigna Local Plus", "Cigna Local Plus", "not_accepted"},
		{"longer rejected", "SPRING_HILL_MEDICAL", "I have Aetna EPO", "Aetna EPO", "not_accepted"},
		{"government vision", "SPRING_HILL_ROUTINE_VISION", "I have Aetna Medicare PPO", "iCare", "accepted"},
		{"government words separated", "SPRING_HILL_ROUTINE_VISION", "Aetna coverage through Medicare", "iCare", "accepted"},
		{"different products", "SPRING_HILL_MEDICAL", "Cigna HMO or Cigna PPO", "", ""},
		{"different lengths", "SPRING_HILL_MEDICAL", "Cigna PPO or Cigna Open Access", "", ""},
		{"accepted or rejected", "SPRING_HILL_MEDICAL", "Cigna PPO or Cigna Local Plus", "", ""},
		{"corrected and legacy products", "HOLLYWOOD_SWEETWATER", "United Healthcare NHP HMO Access or United Oxford", "", ""},
		{"canonical and corrected shorthand", "HOLLYWOOD_SWEETWATER", "United Healthcare Golden Rule or United Oxford", "", ""},
		{"different carriers", "SPRING_HILL_MEDICAL", "Cigna PPO and Aetna HMO", "", ""},
		{"vision products", "SPRING_HILL_ROUTINE_VISION", "Aetna Medicare or VSP", "", ""},
		{"unknown", "SPRING_HILL_MEDICAL", "An unknown insurer", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := participationMatch(tc.source, tc.query)
			if tc.status == "" {
				if got != nil {
					t.Fatalf("got %+v; want clarification", got)
				}
				return
			}
			if got == nil || got.Canonical != tc.canonical || got.Status != tc.status {
				t.Fatalf("got %+v; want %s (%s)", got, tc.canonical, tc.status)
			}
		})
	}
}
