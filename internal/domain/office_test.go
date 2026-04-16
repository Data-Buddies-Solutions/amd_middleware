package domain

import "testing"

func TestLookupOffice(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantOK  bool
	}{
		{"spring hill", "+17275919997", "spring_hill", true},
		{"optical eyeworks", "+19542872010", "optical_eyeworks", true},
		{"crystal river", "+13523202007", "crystal_river", true},
		{"unknown phone", "+15551234567", "", false},
		{"empty string", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			office, ok := LookupOffice(tt.input)
			if ok != tt.wantOK {
				t.Errorf("LookupOffice(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				return
			}
			if ok && office.ID != tt.wantID {
				t.Errorf("LookupOffice(%q).ID = %q, want %q", tt.input, office.ID, tt.wantID)
			}
		})
	}
}

func TestDefaultOffice(t *testing.T) {
	office := DefaultOffice()
	if office == nil {
		t.Fatal("DefaultOffice() returned nil")
	}
	if office.ID != "spring_hill" {
		t.Errorf("DefaultOffice().ID = %q, want %q", office.ID, "spring_hill")
	}
	if office.FacilityID != "1568" {
		t.Errorf("DefaultOffice().FacilityID = %q, want %q", office.FacilityID, "1568")
	}
}

func TestOfficeConfig_IsAllowedColumn(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		columnID string
		want     bool
	}{
		{"1513", true},
		{"1598", true},
		{"1551", true},
		{"1550", true},
		{"9999", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.columnID, func(t *testing.T) {
			got := office.IsAllowedColumn(tt.columnID)
			if got != tt.want {
				t.Errorf("IsAllowedColumn(%q) = %v, want %v", tt.columnID, got, tt.want)
			}
		})
	}
}

func TestOfficeConfig_AllowedColumnIDs(t *testing.T) {
	office := DefaultOffice()
	ids := office.AllowedColumnIDs()

	if len(ids) != 4 {
		t.Fatalf("AllowedColumnIDs() len = %d, want 4", len(ids))
	}

	// Check all expected IDs are present
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for _, want := range []string{"1513", "1598", "1551", "1550"} {
		if !idSet[want] {
			t.Errorf("AllowedColumnIDs() missing %q", want)
		}
	}
}

func TestOfficeConfig_ProviderDisplayName(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		profileID string
		want      string
	}{
		{"620", "Dr. Austin Bach"},
		{"2064", "Dr. J. Licht"},
		{"2076", "Dr. D. Noel"},
		{"9999", ""},
	}

	for _, tt := range tests {
		t.Run(tt.profileID, func(t *testing.T) {
			got := office.ProviderDisplayName(tt.profileID)
			if tt.profileID == "620" {
				// Profile 620 maps to both Bach and Bach Overflow columns; accept either.
				if got != "Dr. Austin Bach" && got != "Dr. Austin Bach (Overflow)" {
					t.Errorf("ProviderDisplayName(%q) = %q, want Dr. Austin Bach or Dr. Austin Bach (Overflow)", tt.profileID, got)
				}
			} else if got != tt.want {
				t.Errorf("ProviderDisplayName(%q) = %q, want %q", tt.profileID, got, tt.want)
			}
		})
	}
}

func TestOfficeConfig_FriendlyProviderName(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		input string
		want  []string
	}{
		{"BACH, AUSTIN", []string{"Dr. Austin Bach", "Dr. Austin Bach (Overflow)"}},
		{"LICHT, JONATHAN", []string{"Dr. J. Licht"}},
		{"NOEL, DON HERSHELSON", []string{"Dr. D. Noel"}},
		{"UNKNOWN", []string{"UNKNOWN"}},
		{"", []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := office.FriendlyProviderName(tt.input)
			valid := false
			for _, w := range tt.want {
				if got == w {
					valid = true
					break
				}
			}
			if !valid {
				t.Errorf("FriendlyProviderName(%q) = %q, want one of %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestOfficeConfig_AppointmentColor(t *testing.T) {
	office := DefaultOffice()

	color, ok := office.AppointmentColor(1006)
	if !ok || color != "RED" {
		t.Errorf("AppointmentColor(1006) = (%q, %v), want (RED, true)", color, ok)
	}

	_, ok = office.AppointmentColor(9999)
	if ok {
		t.Error("AppointmentColor(9999) should return false")
	}
}

func TestOpticalEyeworksConfig(t *testing.T) {
	office, ok := LookupOffice("+19542872010")
	if !ok {
		t.Fatal("prod registry should have +19542872010")
	}
	if office.FacilityID != "1505" {
		t.Errorf("Optical Eyeworks FacilityID = %q, want %q", office.FacilityID, "1505")
	}
	if office.DefaultProfileID != "1983" {
		t.Errorf("Optical Eyeworks DefaultProfileID = %q, want %q", office.DefaultProfileID, "1983")
	}
	if !office.IsAllowedColumn("1304") {
		t.Error("Optical Eyeworks should allow column 1304")
	}
	if office.PediatricRouting != RoutingAll {
		t.Errorf("Optical Eyeworks PediatricRouting = %q, want %q", office.PediatricRouting, RoutingAll)
	}
}

func TestInitRegistry(t *testing.T) {
	// Ensure we restore prod after this test
	defer InitRegistry("prod")

	// Dev environment
	InitRegistry("dev")
	office, ok := LookupOffice("+14843989071")
	if !ok {
		t.Fatal("dev registry should have +14843989071")
	}
	if office.FacilityID != "1032" {
		t.Errorf("dev FacilityID = %q, want %q", office.FacilityID, "1032")
	}
	if !office.IsAllowedColumn("1716") {
		t.Error("dev registry should have column 1716 (Bach)")
	}
	if office.IsAllowedColumn("1513") {
		t.Error("dev registry should NOT have prod column 1513")
	}

	// Prod phone should not exist in dev registry
	_, ok = LookupOffice("+17275919997")
	if ok {
		t.Error("dev registry should NOT have prod phone +17275919997")
	}

	// DefaultOffice works in dev
	devDefault := DefaultOffice()
	if devDefault == nil {
		t.Fatal("DefaultOffice() returned nil in dev mode")
	}
	if devDefault.FacilityID != "1032" {
		t.Errorf("dev DefaultOffice().FacilityID = %q, want %q", devDefault.FacilityID, "1032")
	}

	// Prod environment
	InitRegistry("prod")
	office = DefaultOffice()
	if office.FacilityID != "1568" {
		t.Errorf("prod FacilityID = %q, want %q", office.FacilityID, "1568")
	}
	if !office.IsAllowedColumn("1513") {
		t.Error("prod registry should have column 1513 (Bach)")
	}
	if office.IsAllowedColumn("1716") {
		t.Error("prod registry should NOT have dev column 1716")
	}

	// Default (empty string) = prod
	InitRegistry("")
	office = DefaultOffice()
	if office.FacilityID != "1568" {
		t.Errorf("default FacilityID = %q, want %q", office.FacilityID, "1568")
	}
}
