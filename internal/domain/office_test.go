package domain

import (
	"testing"
	"time"
)

func TestLookupOffice(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
		wantOK bool
	}{
		{"spring hill", "+17275919997", "spring_hill", true},
		{"crystal river", "+13523202007", "crystal_river", true},
		{"hollywood phone", "+19542872010", "hollywood", true},
		{"hollywood phone without plus", "19542872010", "hollywood", true},
		{"hollywood ten digit phone", "9542872010", "hollywood", true},
		{"hollywood formatted phone", "(954) 287-2010", "hollywood", true},
		{"hollywood name", "Hollywood", "hollywood", true},
		{"sweetwater phone", "+17864657475", "sweetwater", true},
		{"sweetwater ten digit phone", "7864657475", "sweetwater", true},
		{"sweetwater alternate phone", "+17864654882", "sweetwater", true},
		{"sweetwater name", "sweetwater", "sweetwater", true},
		{"north miami beach optical phone", "+13055095333", "north_miami_beach_optical", true},
		{"north miami beach optical ten digit phone", "3055095333", "north_miami_beach_optical", true},
		{"north miami beach optical name", "North Miami Beach Optical", "north_miami_beach_optical", true},
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

func TestValidOfficeNamesUnique(t *testing.T) {
	names := ValidOfficeNames()
	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			t.Fatalf("ValidOfficeNames returned duplicate %q in %v", name, names)
		}
		seen[name] = true
	}
	for _, want := range []string{"Spring Hill", "Crystal River", "Hollywood", "Sweetwater", "North Miami Beach Optical"} {
		if !seen[want] {
			t.Fatalf("ValidOfficeNames missing %q in %v", want, names)
		}
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

func TestAppointmentLookupOffices(t *testing.T) {
	tests := []struct {
		name   string
		office *OfficeConfig
		want   []string
	}{
		{"spring hill includes crystal river", springHillOffice, []string{"spring_hill", "crystal_river"}},
		{"crystal river includes spring hill", crystalRiverOffice, []string{"spring_hill", "crystal_river"}},
		{"hollywood includes sweetwater", hollywoodOffice, []string{"hollywood", "sweetwater"}},
		{"sweetwater includes hollywood", sweetwaterOffice, []string{"hollywood", "sweetwater"}},
		{"north miami beach optical stays scoped", northMiamiBeachOpticalOffice, []string{"north_miami_beach_optical"}},
		{"unknown office stays scoped", &OfficeConfig{ID: "other"}, []string{"other"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppointmentLookupOffices(tt.office)
			if len(got) != len(tt.want) {
				t.Fatalf("AppointmentLookupOffices() len = %d, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, wantID := range tt.want {
				if got[i].ID != wantID {
					t.Fatalf("AppointmentLookupOffices()[%d].ID = %q, want %q", i, got[i].ID, wantID)
				}
			}
		})
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
		{"1600", true},
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

	if len(ids) != 5 {
		t.Fatalf("AllowedColumnIDs() len = %d, want 5", len(ids))
	}

	// Check all expected IDs are present
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for _, want := range []string{"1513", "1598", "1551", "1550", "1600"} {
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
		{"1983", "Routine Vision - Dr. Melissa Otero"},
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
		{"OTERO, MELISSA", []string{"Routine Vision - Dr. Melissa Otero"}},
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

func TestCanonicalAppointmentTypeID(t *testing.T) {
	defer InitRegistry("prod")

	InitRegistry("prod")
	if got, ok := CanonicalAppointmentTypeID(1007); !ok || got != 1007 {
		t.Fatalf("prod CanonicalAppointmentTypeID(1007) = (%d, %v), want (1007, true)", got, ok)
	}
	if got, ok := CanonicalAppointmentTypeID(18); ok || got != 0 {
		t.Fatalf("prod CanonicalAppointmentTypeID(18) = (%d, %v), want (0, false)", got, ok)
	}

	InitRegistry("dev")
	if got, ok := CanonicalAppointmentTypeID(18); !ok || got != 1007 {
		t.Fatalf("dev CanonicalAppointmentTypeID(18) = (%d, %v), want (1007, true)", got, ok)
	}
	if got, ok := CanonicalAppointmentTypeID(1010); !ok || got != 1010 {
		t.Fatalf("dev CanonicalAppointmentTypeID(1010) = (%d, %v), want (1010, true)", got, ok)
	}
	if got, ok := CanonicalAppointmentTypeID(9999); ok || got != 0 {
		t.Fatalf("dev CanonicalAppointmentTypeID(9999) = (%d, %v), want (0, false)", got, ok)
	}
}

func TestOfficeConfig_AllowsAppointmentType(t *testing.T) {
	springHill := prodOffices["+17275919997"]
	crystalRiver := prodOffices["+13523202007"]
	hollywood := prodOffices["+19542872010"]
	sweetwater := prodOffices["+17864657475"]

	tests := []struct {
		name    string
		office  *OfficeConfig
		typeID  int
		routing RoutingRule
		want    bool
	}{
		{"spring hill medical accepts medical type", springHill, 1006, RoutingAll, true},
		{"spring hill optical accepts vision type", springHill, 1010, RoutingOpticalOnly, true},
		{"spring hill optical rejects medical type", springHill, 1006, RoutingOpticalOnly, false},
		{"spring hill medical rejects vision type", springHill, 1010, RoutingAll, false},
		{"spring hill rejects crystal river new patient type", springHill, 6167, RoutingAll, false},
		{"crystal river accepts cr new patient type", crystalRiver, 6167, RoutingAll, true},
		{"crystal river accepts cr post op type", crystalRiver, 6168, RoutingAll, true},
		{"crystal river accepts cr established type", crystalRiver, 6169, RoutingAll, true},
		{"crystal river rejects spring hill medical type", crystalRiver, 1006, RoutingAll, false},
		{"crystal river rejects optical routing", crystalRiver, 1010, RoutingOpticalOnly, false},
		{"hollywood medical accepts spring hill medical type", hollywood, 1006, RoutingAll, true},
		{"hollywood routine accepts vision type", hollywood, 1010, RoutingOpticalOnly, true},
		{"hollywood medical rejects vision type", hollywood, 1010, RoutingAll, false},
		{"sweetwater routine accepts vision type", sweetwater, 3364, RoutingOpticalOnly, true},
		{"sweetwater medical rejects crystal river type", sweetwater, 6167, RoutingAll, false},
		{"north miami beach optical routine accepts vision type", northMiamiBeachOpticalOffice, 1010, RoutingOpticalOnly, true},
		{"north miami beach optical rejects medical type", northMiamiBeachOpticalOffice, 1006, RoutingAll, false},
		{"unknown type rejected", springHill, 9999, RoutingAll, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.office.AllowsAppointmentType(tt.typeID, tt.routing)
			if got != tt.want {
				t.Errorf("AllowsAppointmentType(%d, %q) = %v, want %v", tt.typeID, tt.routing, got, tt.want)
			}
		})
	}
}

func TestOfficeConfig_HollywoodAndSweetwaterColumns(t *testing.T) {
	tests := []struct {
		name       string
		office     *OfficeConfig
		facilityID string
		columns    []string
		optical    []string
	}{
		{
			name:       "hollywood",
			office:     prodOffices["+19542872010"],
			facilityID: "1480",
			columns:    []string{"1268", "1478", "1555", "1510", "1305"},
			optical:    []string{"1555", "1510", "1305"},
		},
		{
			name:       "sweetwater",
			office:     prodOffices["+17864657475"],
			facilityID: "670",
			columns:    []string{"682", "1307", "1296", "1554", "1210"},
			optical:    []string{"1296", "1554", "1210"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.office.FacilityID != tt.facilityID {
				t.Fatalf("FacilityID = %q, want %q", tt.office.FacilityID, tt.facilityID)
			}
			for _, columnID := range tt.columns {
				if !tt.office.IsAllowedColumn(columnID) {
					t.Fatalf("expected column %s to be allowed", columnID)
				}
			}
			optical := tt.office.ColumnsForRouting(RoutingOpticalOnly)
			for _, columnID := range tt.optical {
				if !optical[columnID] {
					t.Fatalf("expected optical routing to include column %s", columnID)
				}
			}
		})
	}
}

func TestOfficeConfig_NorthMiamiBeachOpticalColumn(t *testing.T) {
	office := prodOffices["+13055095333"]
	if office == nil {
		t.Fatal("expected North Miami Beach Optical office")
	}
	if office.FacilityID != "1582" {
		t.Fatalf("FacilityID = %q, want 1582", office.FacilityID)
	}
	if office.DefaultProfileID != "621" {
		t.Fatalf("DefaultProfileID = %q, want 621", office.DefaultProfileID)
	}
	if !office.IsAllowedColumn("1601") {
		t.Fatal("expected column 1601 to be allowed")
	}
	if medical := office.ColumnsForRouting(RoutingAll); len(medical) != 0 {
		t.Fatalf("medical routing columns = %v, want none", medical)
	}
	optical := office.ColumnsForRouting(RoutingOpticalOnly)
	if len(optical) != 1 || !optical["1601"] {
		t.Fatalf("optical routing columns = %v, want only 1601", optical)
	}
	col := office.Columns["1601"]
	if col.ProfileID != "621" || col.DisplayName != "Brightview" || col.SameStartCapacity != 0 {
		t.Fatalf("column 1601 = %+v, want Brightview profile 621 single-booked", col)
	}
	if got := office.ProviderDisplayName("621"); got != "Brightview" {
		t.Fatalf("ProviderDisplayName(621) = %q, want Brightview", got)
	}
	if got := office.FriendlyProviderName("BACH, MIRIAM"); got != "Brightview" {
		t.Fatalf("FriendlyProviderName(BACH, MIRIAM) = %q, want Brightview", got)
	}
}

func TestOfficeConfig_SameStartCapacityScope(t *testing.T) {
	tests := []struct {
		name       string
		office     *OfficeConfig
		doubleBook []string
		singleBook []string
	}{
		{
			name:       "spring hill medical columns double-bookable",
			office:     springHillOffice,
			doubleBook: []string{"1513", "1598", "1551", "1550"},
			singleBook: []string{"1600"},
		},
		{
			name:       "crystal river remains single-booked",
			office:     crystalRiverOffice,
			singleBook: []string{"1593"},
		},
		{
			name:       "sweetwater all columns have same-start capacity",
			office:     sweetwaterOffice,
			doubleBook: []string{"682", "1307", "1296", "1554", "1210"},
		},
		{
			name:       "hollywood all columns have same-start capacity",
			office:     hollywoodOffice,
			doubleBook: []string{"1268", "1478", "1555", "1510", "1305"},
		},
		{
			name:       "dev spring hill all columns double-bookable",
			office:     devSpringHillOffice,
			doubleBook: []string{"1716", "1723", "1726"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, columnID := range tt.doubleBook {
				col, ok := tt.office.Columns[columnID]
				if !ok {
					t.Fatalf("missing column %s", columnID)
				}
				if col.SameStartCapacity != 2 {
					t.Fatalf("column %s SameStartCapacity = %d, want 2", columnID, col.SameStartCapacity)
				}
			}
			for _, columnID := range tt.singleBook {
				col, ok := tt.office.Columns[columnID]
				if !ok {
					t.Fatalf("missing column %s", columnID)
				}
				if col.SameStartCapacity != 0 {
					t.Fatalf("column %s SameStartCapacity = %d, want default 0", columnID, col.SameStartCapacity)
				}
			}
		})
	}
}

func TestOfficeConfig_HollywoodSweetwaterRoutineSameStartWindows(t *testing.T) {
	tests := []struct {
		name       string
		office     *OfficeConfig
		routineIDs []string
		medicalIDs []string
	}{
		{
			name:       "sweetwater",
			office:     sweetwaterOffice,
			routineIDs: []string{"1296", "1554", "1210"},
			medicalIDs: []string{"682", "1307"},
		},
		{
			name:       "hollywood",
			office:     hollywoodOffice,
			routineIDs: []string{"1555", "1510", "1305"},
			medicalIDs: []string{"1268", "1478"},
		},
	}

	monday := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	friday := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		start time.Time
		want  int
	}{
		{"monday morning start", monday.Add(8*time.Hour + 30*time.Minute), 2},
		{"monday morning end", monday.Add(10*time.Hour + 45*time.Minute), 2},
		{"monday midday blocked", monday.Add(11 * time.Hour), 0},
		{"monday afternoon start", monday.Add(13*time.Hour + 30*time.Minute), 2},
		{"monday afternoon end", monday.Add(14*time.Hour + 30*time.Minute), 2},
		{"monday late afternoon blocked", monday.Add(14*time.Hour + 45*time.Minute), 0},
		{"friday morning end", friday.Add(11*time.Hour + 45*time.Minute), 2},
		{"friday afternoon blocked", friday.Add(13*time.Hour + 30*time.Minute), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, columnID := range tt.routineIDs {
				col := tt.office.Columns[columnID]
				if len(col.SameStartWindows) == 0 {
					t.Fatalf("routine column %s has no same-start windows", columnID)
				}
				for _, tc := range cases {
					if got := col.SameStartCapacityAt(tc.start); got != tc.want {
						t.Fatalf("%s column %s SameStartCapacityAt(%s) = %d, want %d", tc.name, columnID, tc.start.Format("Monday 15:04"), got, tc.want)
					}
				}
			}
			for _, columnID := range tt.medicalIDs {
				col := tt.office.Columns[columnID]
				if len(col.SameStartWindows) != 0 {
					t.Fatalf("medical column %s should not have time-limited same-start windows", columnID)
				}
			}
		})
	}
}

func TestOfficeConfig_RoutineAgeRules(t *testing.T) {
	office := prodOffices["+17864657475"]
	now := time.Now()

	if got := office.ProvidersForRoutingAndDOB(RoutingOpticalOnly, ""); len(got) != 0 {
		t.Fatalf("missing DOB routine providers = %v, want none", got)
	}
	if got := office.ProvidersForRoutingAndDOB(RoutingOpticalOnly, "not-a-date"); len(got) != 0 {
		t.Fatalf("invalid DOB routine providers = %v, want none", got)
	}
	if !office.ColumnAllowsDOB("682", "") {
		t.Fatal("Bach column should allow missing DOB because it has no minimum age")
	}
	if office.ColumnAllowsDOB("1296", "") {
		t.Fatal("Casas column should require DOB because it has a minimum age")
	}

	tests := []struct {
		name      string
		age       int
		wantNames []string
	}{
		{"age 3 has no routine providers", 3, []string{}},
		{"age 4 can see Calero", 4, []string{"Dr. Calero"}},
		{"age 5 can see Farnan and Calero", 5, []string{"Dr. Farnan", "Dr. Calero"}},
		{"age 7 can see all routine providers", 7, []string{"Dr. Casas", "Dr. Farnan", "Dr. Calero"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dob := now.AddDate(-tt.age, 0, 0).Format("01/02/2006")
			got := office.ProvidersForRoutingAndDOB(RoutingOpticalOnly, dob)
			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %v, want %v", got, tt.wantNames)
			}
			for i, want := range tt.wantNames {
				if got[i] != want {
					t.Fatalf("got %v, want %v", got, tt.wantNames)
				}
			}
		})
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
	if !office.IsAllowedColumn("1600") {
		t.Error("prod registry should have column 1600 (Routine Vision)")
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
