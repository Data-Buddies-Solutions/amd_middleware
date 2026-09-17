package domain

import (
	"testing"
)

func TestLookupInsurance_SelfPayMedical(t *testing.T) {
	entry, found := LookupInsuranceForCoverageAtOffice("self-pay", InsuranceModeMedical, &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"})
	if !found {
		t.Fatal("self-pay medical found = false, want true")
	}
	if entry.CarrierID != "car301672" || entry.Routing != RoutingAll {
		t.Fatalf("self-pay medical entry = %#v, want car301672/all", entry)
	}
}

func TestLookupInsurance_SunshineHealthRoutineVision(t *testing.T) {
	entry, found := LookupInsuranceForCoverageAtOffice("Sunshine Health", InsuranceModeVision, &OfficeConfig{ID: "hollywood", DisplayName: "Hollywood"})
	if !found {
		t.Fatal("Sunshine Health routine vision found = false, want true")
	}
	if entry.CarrierID != "car281245" || entry.Routing != RoutingOpticalOnly {
		t.Fatalf("Sunshine Health routine vision entry = %#v, want car281245/optical_only", entry)
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

func TestLookupInsurance_HollywoodSweetwaterMedicalABachOverrides(t *testing.T) {
	hollywood := &OfficeConfig{ID: "hollywood", DisplayName: "Hollywood"}
	sweetwater := &OfficeConfig{ID: "sweetwater", DisplayName: "Sweetwater"}

	tests := []struct {
		name          string
		office        *OfficeConfig
		input         string
		wantCarrierID string
		wantPreauth   bool
	}{
		{"hollywood accepts aetna epo university", hollywood, "Aetna EPO University of Miami", "car40887", false},
		{"hollywood accepts aetna epo north broward sheet name", hollywood, "Aetna EPO Plan / North Broward Hospital", "car40887", false},
		{"hollywood accepts avmed select sheet name", hollywood, "AvMed Select, Broad Network, TIER B", "car40890", false},
		{"hollywood accepts florida blue hmo via emi", hollywood, "Florida Blue HMO", "car280750", true},
		{"hollywood accepts careplus medical through premier", hollywood, "CarePlus", "car281317", true},
		{"hollywood accepts cigna medicare advantage through healthspring", hollywood, "Cigna Medicare Advantage", "car302890", true},
		{"hollywood accepts cigna medicare advantage ppo without preauth", hollywood, "Cigna Medicare Advantage PPO", "car302890", false},
		{"hollywood accepts preferred care partners through its own carrier", hollywood, "Preferred Care Partners", "car40916", false},
		{"hollywood accepts global alias only when canonical is in abach map", hollywood, "Blue Cross", "car40897", false},
		{"hollywood accepts miami childrens medical", hollywood, "Miami Children's Health Plan (Medicaid) Medical", "car40907", false},
		{"hollywood accepts humana medicaid hmo with preauth", hollywood, "Humana Medicaid HMO", "car303033", true},
		{"hollywood accepts florida complete care medical", hollywood, "Florida Complete Care - Medicare Medical ONLY", "car40907", false},
		{"hollywood accepts florida community care medical", hollywood, "Florida Community Care (ILF Medicaid", "car40907", false},
		{"hollywood accepts meritain aetna sheet name", hollywood, "Meritain Health - Aetna", "car301578", false},
		{"hollywood accepts preferred care network sheet name", hollywood, "Preferred Care Network Preferred Care Partners", "car40916", false},
		{"hollywood accepts united individual exchange network with preauth", hollywood, "United Healthcare Individual Exchange Network (Medical)", "car40923", true},
		{"hollywood accepts united global with preauth", hollywood, "United Healthcare Global (Medical) International Plan", "", true},
		{"hollywood accepts umr sheet name", hollywood, "UMR (United Health One)", "car284838", false},
		{"hollywood accepts tricare prime sheet name with preauth", hollywood, "Tricare Humana Military (Prime)", "car284327", true},
		{"hollywood accepts wellcare medicare lppo with preauth", hollywood, "Wellcare Medicare LPPO Medical", "car281317", true},
		{"sweetwater maps aetna medicare ppo to icare", sweetwater, "Aetna Medicare PPO", "car40907", false},
		{"sweetwater accepts doctors health medicare", sweetwater, "Doctors Health Medicare", "car40907", false},
		{"sweetwater accepts devoted through premier", sweetwater, "Devoted", "car281317", false},
		{"sweetwater accepts solis with preauth", sweetwater, "Solis Medicare", "car281317", true},
		{"hollywood accepts self pay alias", hollywood, "self-pay", "car301672", false},
		{"sweetwater accepts cash pay alias", sweetwater, "Cash Pay", "car301672", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, found := LookupInsuranceForCoverageAtOffice(tt.input, InsuranceModeMedical, tt.office)
			if !found {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) found = false, want true", tt.input)
			}
			if entry.CarrierID != tt.wantCarrierID {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) carrierID = %q, want %q", tt.input, entry.CarrierID, tt.wantCarrierID)
			}
			if entry.Routing != RoutingBachOnly {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) routing = %q, want %q", tt.input, entry.Routing, RoutingBachOnly)
			}
			if entry.PreauthRequired != tt.wantPreauth {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) preauth = %v, want %v", tt.input, entry.PreauthRequired, tt.wantPreauth)
			}
		})
	}
}

func TestLookupInsurance_HollywoodSweetwaterDoNotChangeSpringHillMedical(t *testing.T) {
	springHill := &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"}

	tests := []string{
		"Aetna EPO University of Miami",
		"Doctors Health Medicare",
		"Florida Blue HMO",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			entry, found := LookupInsuranceForCoverageAtOffice(input, InsuranceModeMedical, springHill)
			if !found {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) found = false, want true", input)
			}
			if entry.Routing != RoutingNotAccepted {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) routing = %q, want %q", input, entry.Routing, RoutingNotAccepted)
			}
		})
	}
}

func TestLookupInsurance_HollywoodSweetwaterRejectsNonABachFallbacks(t *testing.T) {
	office := &OfficeConfig{ID: "hollywood", DisplayName: "Hollywood"}

	tests := []string{
		"Cigna",
		"Molina",
		"United Healthcare Choice",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if entry, found := LookupInsuranceForCoverageAtOffice(input, InsuranceModeMedical, office); found {
				t.Fatalf("LookupInsuranceForCoverageAtOffice(%q) = %+v, true; want not found", input, entry)
			}
		})
	}
}

func TestLookupInsurance_HollywoodSweetwaterRejectedMedicalStillRejected(t *testing.T) {
	office := &OfficeConfig{ID: "hollywood", DisplayName: "Hollywood"}

	tests := []string{
		"Cigna Local Plus",
		"Molina Marketplace",
		"Florida BlueSelect",
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
		{"bach only", RoutingBachOnly, []string{"Dr. Bach"}},
		{"bach+licht", RoutingBachLicht, []string{"Dr. Bach", "Dr. Licht"}},
		{"all", RoutingAll, []string{"Dr. Bach", "Dr. Licht", "Dr. Noel"}},
		{"optical only", RoutingOpticalOnly, []string{"Dr. Otero"}},
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
