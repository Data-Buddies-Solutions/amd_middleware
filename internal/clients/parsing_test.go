package clients

import "testing"

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		input  string
		prefix string
		want   string
	}{
		{"col1513", "col", "1513"},
		{"prof620", "prof", "620"},
		{"fac1568", "fac", "1568"},
		{"1513", "col", "1513"},  // no prefix
		{"col", "col", "col"},    // prefix only, no ID
		{"", "col", ""},          // empty
		{"colABC", "col", "ABC"}, // non-numeric
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripPrefix(tt.input, tt.prefix)
			if got != tt.want {
				t.Errorf("stripPrefix(%q, %q) = %q, want %q", tt.input, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestParseWorkweek(t *testing.T) {
	tests := []struct {
		name string
		ww   string
		want int
	}{
		// AMD format: 7 chars for Mon-Sun (1=works, 0=off)
		// Our bitmask: 1=Sun, 2=Mon, 4=Tue, 8=Wed, 16=Thu, 32=Fri, 64=Sat
		{"Mon-Fri", "1111100", 2 + 4 + 8 + 16 + 32},            // 62
		{"Wed-Thu", "0011000", 8 + 16},                         // 24
		{"Every day", "1111111", 1 + 2 + 4 + 8 + 16 + 32 + 64}, // 127
		{"No days", "0000000", 0},
		{"Mon only", "1000000", 2},
		{"Sun only", "0000001", 1},
		{"Sat only", "0000010", 64},
		{"invalid length", "111", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorkweek(tt.ww)
			if got != tt.want {
				t.Errorf("parseWorkweek(%q) = %d, want %d", tt.ww, got, tt.want)
			}
		})
	}
}

func TestNormalizeTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0800", "08:00"},
		{"1700", "17:00"},
		{"08:00", "08:00"}, // already correct
		{"17:00", "17:00"}, // already correct
		{"", ""},           // empty
		{"0930", "09:30"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeTime(tt.input)
			if got != tt.want {
				t.Errorf("normalizeTime(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{
		"@name": "DR. BACH",
		"@id":   "col1513",
		"@num":  42.0,
	}

	if got := getString(m, "@name"); got != "DR. BACH" {
		t.Errorf("getString(@name) = %q, want 'DR. BACH'", got)
	}
	if got := getString(m, "@id"); got != "col1513" {
		t.Errorf("getString(@id) = %q, want 'col1513'", got)
	}
	if got := getString(m, "@missing"); got != "" {
		t.Errorf("getString(@missing) = %q, want ''", got)
	}
	// Non-string value
	if got := getString(m, "@num"); got != "" {
		t.Errorf("getString(@num) = %q, want ''", got)
	}
}

func TestGetInt(t *testing.T) {
	m := map[string]interface{}{
		"@interval": "15",
		"@max":      float64(2),
		"@zero":     "0",
		"@missing":  nil,
	}

	if got := getInt(m, "@interval"); got != 15 {
		t.Errorf("getInt(@interval) = %d, want 15", got)
	}
	if got := getInt(m, "@max"); got != 2 {
		t.Errorf("getInt(@max) = %d, want 2", got)
	}
	if got := getInt(m, "@zero"); got != 0 {
		t.Errorf("getInt(@zero) = %d, want 0", got)
	}
	if got := getInt(m, "@nonexistent"); got != 0 {
		t.Errorf("getInt(@nonexistent) = %d, want 0", got)
	}
}

func TestParseColumns_SingleColumn(t *testing.T) {
	data := map[string]interface{}{
		"@id":       "col1513",
		"@name":     "DR. BACH - BP",
		"@profile":  "prof620",
		"@facility": "fac1568",
		"columnsetting": map[string]interface{}{
			"@start":           "0800",
			"@end":             "1700",
			"@interval":        "15",
			"@maxapptsperslot": "0",
			"@workweek":        "1111100",
		},
	}

	columns := parseColumns(data)

	if len(columns) != 1 {
		t.Fatalf("Expected 1 column, got %d", len(columns))
	}

	col := columns[0]
	if col.ID != "1513" {
		t.Errorf("ID = %q, want '1513'", col.ID)
	}
	if col.Name != "DR. BACH - BP" {
		t.Errorf("Name = %q, want 'DR. BACH - BP'", col.Name)
	}
	if col.ProfileID != "620" {
		t.Errorf("ProfileID = %q, want '620'", col.ProfileID)
	}
	if col.FacilityID != "1568" {
		t.Errorf("FacilityID = %q, want '1568'", col.FacilityID)
	}
	if col.Interval != 15 {
		t.Errorf("Interval = %d, want 15", col.Interval)
	}
	if col.StartTime != "08:00" {
		t.Errorf("StartTime = %q, want '08:00'", col.StartTime)
	}
}

func TestParseColumns_MultipleColumns(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{
			"@id":       "col1513",
			"@name":     "DR. BACH - BP",
			"@profile":  "prof620",
			"@facility": "fac1568",
			"columnsetting": map[string]interface{}{
				"@start":    "0800",
				"@end":      "1700",
				"@interval": "15",
				"@workweek": "1111100",
			},
		},
		map[string]interface{}{
			"@id":       "col1551",
			"@name":     "DR. LICHT",
			"@profile":  "prof2064",
			"@facility": "fac1568",
			"columnsetting": map[string]interface{}{
				"@start":    "0900",
				"@end":      "1230",
				"@interval": "15",
				"@workweek": "0011000",
			},
		},
	}

	columns := parseColumns(data)
	if len(columns) != 2 {
		t.Fatalf("Expected 2 columns, got %d", len(columns))
	}
	if columns[0].ID != "1513" {
		t.Errorf("First column ID = %q, want '1513'", columns[0].ID)
	}
	if columns[1].ID != "1551" {
		t.Errorf("Second column ID = %q, want '1551'", columns[1].ID)
	}
}

func TestParseColumns_Nil(t *testing.T) {
	columns := parseColumns(nil)
	if columns != nil {
		t.Errorf("Expected nil for nil input, got %v", columns)
	}
}

func TestParseProfiles(t *testing.T) {
	// Single profile
	single := map[string]interface{}{
		"@id":   "prof620",
		"@code": "ABCH",
		"@name": "BACH, AUSTIN",
	}
	profiles := parseProfiles(single)
	if len(profiles) != 1 {
		t.Fatalf("Expected 1 profile, got %d", len(profiles))
	}
	if profiles[0].ID != "620" {
		t.Errorf("Profile ID = %q, want '620'", profiles[0].ID)
	}
	if profiles[0].Name != "BACH, AUSTIN" {
		t.Errorf("Profile Name = %q, want 'BACH, AUSTIN'", profiles[0].Name)
	}

	// Multiple profiles
	multi := []interface{}{
		map[string]interface{}{"@id": "prof620", "@code": "ABCH", "@name": "BACH, AUSTIN"},
		map[string]interface{}{"@id": "prof2064", "@code": "ALIC", "@name": "LICHT, J"},
	}
	profiles = parseProfiles(multi)
	if len(profiles) != 2 {
		t.Fatalf("Expected 2 profiles, got %d", len(profiles))
	}

	// Nil
	profiles = parseProfiles(nil)
	if profiles != nil {
		t.Errorf("Expected nil for nil input")
	}
}

func TestParseFacilities(t *testing.T) {
	single := map[string]interface{}{
		"@id":   "fac1568",
		"@code": "ABSPR",
		"@name": "ABITA EYE GROUP SPRING HILL",
	}
	facilities := parseFacilities(single)
	if len(facilities) != 1 {
		t.Fatalf("Expected 1 facility, got %d", len(facilities))
	}
	if facilities[0].ID != "1568" {
		t.Errorf("Facility ID = %q, want '1568'", facilities[0].ID)
	}
	if facilities[0].Name != "ABITA EYE GROUP SPRING HILL" {
		t.Errorf("Facility Name = %q, want 'ABITA EYE GROUP SPRING HILL'", facilities[0].Name)
	}

	// Nil
	facilities = parseFacilities(nil)
	if facilities != nil {
		t.Errorf("Expected nil for nil input")
	}
}
