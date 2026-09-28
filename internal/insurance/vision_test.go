package insurance

import (
	"advancedmd-token-management/internal/domain"
	"testing"
)

func TestLookupInsuranceForCoverage_RoutineVision(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		wantFound bool
	}{
		{"top level VSP", "VSP", "car280695", true},
		{"top level Oscar", "Oscar", "car284233", true},
		{"top level self pay", "Self Pay", "car301672", true},
		{"misspelled Solstice", "Soltice", "car301652", true},
		{"VSP alias", "Lincoln Finacial", "car280695", true},
		{"EyeMed alias", "Humana", "car280684", true},
		{"Davis alias", "Florida Blue", "car280612", true},
		{"Spectera alias", "United Health Care", "car308790", true},
		{"iCare exact", "iCare", "car40907", true},
		{"iCare spaced", "i Care", "car40907", true},
		{"iCare speech recognition", "Eye Care", "car40907", true},
		{"iCare alias", "Simply Medcaid", "car40907", true},
		{"Aetna commercial stays EyeMed", "Aetna", "car280684", true},
		{"Aetna Medicaid uses iCare", "Aetna Medicaid", "car40907", true},
		{"Aetna Medicare uses iCare", "Aetna Medicare", "car40907", true},
		{"Aetna government punctuation uses iCare", "Aetna Better Health (Medicaid)", "car40907", true},
		{"Aetna government word order uses iCare", "Medicare Advantage by Aetna", "car40907", true},
		{"Aetna government rule overrides another carrier phrase", "Aetna Medicare WellCare Medicare HMO (Vision)", "car40907", true},
		{"Alivi exact", "Alivi", "car308796", true},
		{"pending CarePlus routine vision", "CarePlus", "", false},
		{"pending CarePlus Medicare routine vision", "CarePlus (Medicare) Vision", "", false},
		{"medical not accepted becomes vision bucket", "Optimum", "car40907", true},
		{"Abita Aetna Medicare PPO vision", "Aetna Medicare PPO (Vision) effective 1/1/2026", "car40907", true},
		{"Abita Aetna Medicare vision", "Aetna Medicare HMO & PPO (Vision)", "car40907", true},
		{"Abita Aetna Better Health vision", "Aetna Better Health Medicaid MMA (Vision)", "car40907", true},
		{"Abita Aetna Healthy Kids vision", "Aetna Healthy Kids/Kid Care (CHIP) (Vision)", "car40907", true},
		{"Abita Ambetter vision", "Ambetter (Vision)", "car281245", true},
		{"Abita AvMed Entrust vision", "AvMed Entrust (Vision)", "car40907", true},
		{"Abita Children's Medical Services vision", "Children's Medical Services (Vision)", "car281245", true},
		{"Abita Community Care Plan vision", "Community Care Plan Vision", "car40907", true},
		{"Abita Devoted HMO vision", "Devoted Medicare HMO (Vision)", "car281317", true},
		{"Abita Devoted PPO vision", "Devoted Medicare PPO (Vision)", "car281317", true},
		{"Abita Doctors Health vision", "Doctors Health Medicare (Vision) EFFECTIVE 8/1/2023", "car40907", true},
		{"Abita Florida Blue Medicare vision", "Florida Blue Medicare HMO & PPO (Vision)", "car281317", true},
		{"Abita Freedom Health vision", "Freedom Health Medicare (Vision)", "car40907", true},
		{"Abita Healthsun vision", "Healthsun Vision ONLY", "car40907", true},
		{"Abita Humana Medicaid vision", "Humana (Medicaid) Vision", "car40907", true},
		{"Abita Humana Medicare vision", "Humana (Medicare) Vision", "car40907", true},
		{"Abita Miami Children's vision", "Miami Children's Health Plan (Medicaid) Vision", "car40907", true},
		{"Abita Molina vision", "Molina Medicaid (Vision)", "car40907", true},
		{"Abita Optimum vision", "Optimum Healthplan Medicare (Vision)", "car40907", true},
		{"Abita Preferred Care Network vision", "Preferred Care Network - Previously Medica (Vision)", "car40907", true},
		{"Abita Simply Medicaid vision", "Simply Medicaid/Healthy Kids (Vision)", "car40907", true},
		{"Abita Simply Medicare vision", "Simply Medicare (Vision)", "car40907", true},
		{"Abita Solis Medicare vision", "Solis Medicare (Vision)", "car281317", true},
		{"Abita Staywell vision", "Staywell Medicaid (Vision)", "car281245", true},
		{"Abita Sunshine vision", "Sunshine Medicaid (Vision)", "car281245", true},
		{"Abita WellCare Medicaid vision", "Wellcare (Medicaid) Vision", "car281245", true},
		{"Abita WellCare Medicare vision", "WellCare Medicare HMO (Vision)", "car281317", true},
		{"unknown carrier", "unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, gotFound := lookupVisionInsurance(tt.input)
			if gotFound != tt.wantFound {
				t.Errorf("LookupInsuranceForCoverage(%q, vision) found = %v, want %v", tt.input, gotFound, tt.wantFound)
			}
			if gotFound {
				if entry.CarrierID != tt.wantID {
					t.Errorf("LookupInsuranceForCoverage(%q, vision) carrierID = %q, want %q", tt.input, entry.CarrierID, tt.wantID)
				}
				if entry.Routing != domain.RoutingOpticalOnly {
					t.Errorf("LookupInsuranceForCoverage(%q, vision) routing = %q, want %q", tt.input, entry.Routing, domain.RoutingOpticalOnly)
				}
			}
		})
	}
}
