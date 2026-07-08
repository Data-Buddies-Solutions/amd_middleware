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
		{"all - Self Pay", "car301672", RoutingAll, false},
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
		{"hollywood accepts preferred care partners through uhc", hollywood, "Preferred Care Partners", "car40923", false},
		{"hollywood accepts global alias only when canonical is in abach map", hollywood, "Blue Cross", "car40897", false},
		{"hollywood accepts miami childrens medical", hollywood, "Miami Children's Health Plan (Medicaid) Medical", "car40907", false},
		{"hollywood accepts humana medicaid hmo with preauth", hollywood, "Humana Medicaid HMO", "car308175", true},
		{"hollywood accepts florida complete care medical", hollywood, "Florida Complete Care - Medicare Medical ONLY", "car40907", false},
		{"hollywood accepts florida community care medical", hollywood, "Florida Community Care (ILF Medicaid", "car40907", false},
		{"hollywood accepts meritain aetna sheet name", hollywood, "Meritain Health - Aetna", "car301578", false},
		{"hollywood accepts preferred care network sheet name", hollywood, "Preferred Care Network Preferred Care Partners", "car40923", false},
		{"hollywood accepts united individual exchange network with preauth", hollywood, "United Healthcare Individual Exchange Network (Medical)", "car40923", true},
		{"hollywood accepts united global with preauth", hollywood, "United Healthcare Global (Medical) International Plan", "car284971", true},
		{"hollywood accepts umr sheet name", hollywood, "UMR (United Health One)", "car40923", false},
		{"hollywood accepts tricare prime sheet name with preauth", hollywood, "Tricare Humana Military (Prime)", "car40921", true},
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

func TestRoutingForCarrierIDAtOffice_HollywoodSweetwaterAcceptedCarriers(t *testing.T) {
	hollywood := &OfficeConfig{ID: "hollywood", DisplayName: "Hollywood"}
	springHill := &OfficeConfig{ID: "spring_hill", DisplayName: "Spring Hill"}

	routing, ambiguous := RoutingForCarrierIDAtOffice("car280750", hollywood)
	if routing != RoutingBachOnly || ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car280750, Hollywood) = %q, %v; want %q, false", routing, ambiguous, RoutingBachOnly)
	}

	routing, ambiguous = RoutingForCarrierIDAtOffice("car40923", hollywood)
	if routing != RoutingBachOnly || !ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car40923, Hollywood) = %q, %v; want %q, true", routing, ambiguous, RoutingBachOnly)
	}

	routing, ambiguous = RoutingForCarrierIDAtOffice("car301672", hollywood)
	if routing != RoutingBachOnly || ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car301672, Hollywood) = %q, %v; want %q, false", routing, ambiguous, RoutingBachOnly)
	}

	routing, ambiguous = RoutingForCarrierIDAtOffice("car280750", springHill)
	if routing != RoutingNotAccepted || ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car280750, Spring Hill) = %q, %v; want %q, false", routing, ambiguous, RoutingNotAccepted)
	}

	routing, ambiguous = RoutingForCarrierIDAtOffice("car99999", hollywood)
	if routing != RoutingNotAccepted || ambiguous {
		t.Fatalf("RoutingForCarrierIDAtOffice(car99999, Hollywood) = %q, %v; want %q, false", routing, ambiguous, RoutingNotAccepted)
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

func TestRoutingForDemographicInsurance_HollywoodSweetwaterABachPolicy(t *testing.T) {
	hollywood := &OfficeConfig{ID: "hollywood", DisplayName: "Hollywood"}

	tests := []struct {
		name          string
		carrierID     string
		carrierName   string
		wantRouting   RoutingRule
		wantAmbiguous bool
	}{
		{"exact carrier name accepted even if globally rejected", "car301345", "Cigna Miami-Dade Public Schools", RoutingBachOnly, false},
		{"generic cigna carrier falls back to accepted carrier id", "car301345", "Cigna", RoutingBachOnly, true},
		{"non abach exact plan does not fall back to accepted carrier id", "car40923", "United Healthcare Choice", RoutingNotAccepted, false},
		{"preferred care legacy carrier accepted by id", "car40916", "", RoutingBachOnly, false},
		{"self pay carrier accepted by id", "car301672", "", RoutingBachOnly, false},
		{"self pay exact name accepted", "car301672", "Self Pay", RoutingBachOnly, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routing, ambiguous := RoutingForDemographicInsurance(tt.carrierID, tt.carrierName, hollywood)
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
