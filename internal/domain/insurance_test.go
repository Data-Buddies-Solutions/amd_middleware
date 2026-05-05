package domain

import (
	"strings"
	"testing"
)

func TestRoutingForCarrierID(t *testing.T) {
	tests := []struct {
		name          string
		carrierID     string
		wantRouting   RoutingRule
		wantAmbiguous bool
	}{
		// Unambiguous carriers
		{"not accepted - Doctors Healthcare", "car281648", RoutingNotAccepted, false},
		{"not accepted - Preferred Care", "car40916", RoutingNotAccepted, false},
		{"bach only - Humana Medicaid", "car303033", RoutingBachOnly, false},
		{"bach only - Humana Medicare", "car40906", RoutingBachOnly, false},
		{"bach only - Meritain Health", "car301578", RoutingBachOnly, false},
		{"bach+licht - AvMed", "car40890", RoutingBachLicht, false},
		{"bach+licht - Tricare East", "car284327", RoutingBachLicht, false},
		{"bach+licht - Oscar", "car284233", RoutingBachLicht, false},
		{"not accepted - Eye America", "car308627", RoutingNotAccepted, false},

		// Ambiguous carriers — default to RoutingAll with ambiguous flag
		{"ambiguous - Aetna", "car40887", RoutingAll, true},
		{"ambiguous - FL Blue", "car40897", RoutingAll, true},
		{"ambiguous - iCare", "car40907", RoutingAll, true},
		{"ambiguous - Molina", "car40912", RoutingAll, true},
		{"ambiguous - UHC", "car40923", RoutingAll, true},
		{"ambiguous - Cigna HMO", "car301345", RoutingAll, true},
		{"ambiguous - Humana consolidated", "car308175", RoutingAll, true},

		// Unknown carrier — defaults to RoutingAll, not ambiguous
		{"unknown carrier defaults to all", "car99999", RoutingAll, false},
		{"empty carrier defaults to all", "", RoutingAll, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routing, ambiguous := RoutingForCarrierID(tt.carrierID)
			if routing != tt.wantRouting {
				t.Errorf("RoutingForCarrierID(%q) routing = %q, want %q", tt.carrierID, routing, tt.wantRouting)
			}
			if ambiguous != tt.wantAmbiguous {
				t.Errorf("RoutingForCarrierID(%q) ambiguous = %v, want %v", tt.carrierID, ambiguous, tt.wantAmbiguous)
			}
		})
	}
}

func TestLookupInsurance_SpringHillRejectedMedicalPlans(t *testing.T) {
	office := &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"}
	tests := []string{
		"Aetna EPO",
		"Humana Gold Plus",
		"Miami Children's",
		"Humana Medicaid",
		"Fl Blue Select",
		"Cigna",
		"Miami Dade Doctors Health",
		"Av Med Medicare Advantage",
		"Cigna Local Plus",
		"Eye America",
		"Fl Blue HMO",
		"Fl Blue Steward",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			entry, found := LookupInsuranceForCoverageAtOffice(input, InsuranceModeMedical, office)
			if !found {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) found = false, want true", input)
			}
			if entry.Routing != RoutingNotAccepted {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) routing = %q, want %q", input, entry.Routing, RoutingNotAccepted)
			}
		})
	}
}

func TestLookupInsurance_CrystalRiverRejectedMedicalPlans(t *testing.T) {
	office := &OfficeConfig{ID: "crystal_river", DisplayName: "Crystal River"}
	tests := []string{
		"Medicaid",
		"Florida Medicaid",
		"Molina Medicaid",
		"Aetna Better Health",
		"Staywell",
		"Sunshine",
		"Ambetter",
		"Ambetter Select",
		"Simply Medicaid",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			entry, found := LookupInsuranceForCoverageAtOffice(input, InsuranceModeMedical, office)
			if !found {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) found = false, want true", input)
			}
			if entry.Routing != RoutingNotAccepted {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) routing = %q, want %q", input, entry.Routing, RoutingNotAccepted)
			}
		})
	}
}

func TestLookupInsurance_CrystalRiverExtrasRemainAcceptedAtSpringHill(t *testing.T) {
	office := &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"}
	tests := []string{
		"Medicaid",
		"Ambetter",
		"Simply Medicaid",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			entry, found := LookupInsuranceForCoverageAtOffice(input, InsuranceModeMedical, office)
			if !found {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) found = false, want true", input)
			}
			if entry.Routing == RoutingNotAccepted {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) routing = %q, want accepted routing", input, entry.Routing)
			}
		})
	}
}

func TestRoutingForCarrierIDAtOffice_CrystalRiverRejectedCarriers(t *testing.T) {
	crystalRiver := &OfficeConfig{ID: "crystal_river", DisplayName: "Crystal River"}
	springHill := &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"}

	routing, ambiguous := RoutingForCarrierIDAtOffice("car281245", crystalRiver)
	if routing != RoutingNotAccepted || ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car281245, Crystal River) = %q, %v; want %q, false", routing, ambiguous, RoutingNotAccepted)
	}

	routing, ambiguous = RoutingForCarrierIDAtOffice("car281245", springHill)
	if routing != RoutingAll || ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car281245, Spring Hill) = %q, %v; want %q, false", routing, ambiguous, RoutingAll)
	}
}

func TestRoutingForDemographicInsurance_UsesCarrierNameBeforeCarrierFallback(t *testing.T) {
	crystalRiver := &OfficeConfig{ID: "crystal_river", DisplayName: "Crystal River"}

	tests := []struct {
		name          string
		carrierID     string
		carrierName   string
		wantRouting   RoutingRule
		wantAmbiguous bool
	}{
		{"crystal river rejects exact sunshine name", "car281245", "Sunshine Medicaid", RoutingNotAccepted, false},
		{"crystal river accepts exact wellcare name", "car281245", "Wellcare", RoutingAll, false},
		{"crystal river rejects shared carrier fallback", "car281245", "", RoutingNotAccepted, false},
		{"spring hill preserves generic aetna ambiguity", "car40887", "Aetna", RoutingAll, true},
		{"spring hill preserves generic cigna ambiguity", "car301345", "Cigna", RoutingAll, true},
		{"spring hill uses exact aetna epo rejection", "car40887", "Aetna EPO", RoutingNotAccepted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			office := crystalRiver
			if strings.HasPrefix(tt.name, "spring hill") {
				office = &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"}
			}

			routing, ambiguous := RoutingForDemographicInsurance(tt.carrierID, tt.carrierName, office)
			if routing != tt.wantRouting {
				t.Fatalf("RoutingForDemographicInsurance(%q, %q) routing = %q, want %q", tt.carrierID, tt.carrierName, routing, tt.wantRouting)
			}
			if ambiguous != tt.wantAmbiguous {
				t.Fatalf("RoutingForDemographicInsurance(%q, %q) ambiguous = %v, want %v", tt.carrierID, tt.carrierName, ambiguous, tt.wantAmbiguous)
			}
		})
	}
}

func TestColumnsForRouting(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		name    string
		rule    RoutingRule
		wantLen int
		wantIDs []string
	}{
		{"not accepted returns nil", RoutingNotAccepted, 0, nil},
		{"bach only returns 1513,1598", RoutingBachOnly, 2, []string{"1513", "1598"}},
		{"bach+licht returns 1513,1598,1551", RoutingBachLicht, 3, []string{"1513", "1598", "1551"}},
		{"all returns all", RoutingAll, 4, []string{"1513", "1598", "1551", "1550"}},
		{"optical only returns routine vision", RoutingOpticalOnly, 1, []string{"1600"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols := office.ColumnsForRouting(tt.rule)
			if tt.wantLen == 0 {
				if cols != nil {
					t.Errorf("ColumnsForRouting(%q) = %v, want nil", tt.rule, cols)
				}
				return
			}
			if len(cols) != tt.wantLen {
				t.Errorf("ColumnsForRouting(%q) len = %d, want %d", tt.rule, len(cols), tt.wantLen)
			}
			for _, id := range tt.wantIDs {
				if !cols[id] {
					t.Errorf("ColumnsForRouting(%q) missing column %q", tt.rule, id)
				}
			}
		})
	}
}

func TestProvidersForRouting(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		name      string
		rule      RoutingRule
		wantNames []string
	}{
		{"not accepted returns nil", RoutingNotAccepted, nil},
		{"bach only", RoutingBachOnly, []string{"Dr. Bach", "Dr. Bach"}},
		{"bach+licht", RoutingBachLicht, []string{"Dr. Bach", "Dr. Bach", "Dr. Licht"}},
		{"all", RoutingAll, []string{"Dr. Bach", "Dr. Bach", "Dr. Licht", "Dr. Noel"}},
		{"optical only", RoutingOpticalOnly, []string{"Routine Vision"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := office.ProvidersForRouting(tt.rule)
			if tt.wantNames == nil {
				if names != nil {
					t.Errorf("ProvidersForRouting(%q) = %v, want nil", tt.rule, names)
				}
				return
			}
			if len(names) != len(tt.wantNames) {
				t.Fatalf("ProvidersForRouting(%q) len = %d, want %d", tt.rule, len(names), len(tt.wantNames))
			}
			for i, name := range tt.wantNames {
				if names[i] != name {
					t.Errorf("ProvidersForRouting(%q)[%d] = %q, want %q", tt.rule, i, names[i], name)
				}
			}
		})
	}
}

func TestParseRoutingRule(t *testing.T) {
	tests := []struct {
		input string
		want  RoutingRule
	}{
		{"not_accepted", RoutingNotAccepted},
		{"bach_only", RoutingBachOnly},
		{"bach_licht", RoutingBachLicht},
		{"all_three", RoutingAll},
		{"optical_only", RoutingOpticalOnly},
		{"", RoutingAll},          // default
		{"invalid", RoutingAll},   // default
		{"BACH_ONLY", RoutingAll}, // case sensitive, doesn't match
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseRoutingRule(tt.input)
			if got != tt.want {
				t.Errorf("ParseRoutingRule(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
