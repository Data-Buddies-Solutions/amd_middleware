package domain

import (
	"slices"
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

func TestAppointmentLookupOfficeIDs(t *testing.T) {
	tests := []struct {
		name   string
		env    Environment
		office string
		want   []string
	}{
		{"spring hill includes crystal river", Prod, "spring_hill", []string{"spring_hill", "crystal_river"}},
		{"crystal river includes spring hill", Prod, "crystal_river", []string{"spring_hill", "crystal_river"}},
		{"hollywood includes sweetwater and north miami beach optical", Prod, "hollywood", []string{"hollywood", "sweetwater", "north_miami_beach_optical"}},
		{"sweetwater includes hollywood", Prod, "sweetwater", []string{"hollywood", "sweetwater"}},
		{"north miami beach optical stays scoped", Prod, "north_miami_beach_optical", []string{"north_miami_beach_optical"}},
		{"sandbox spring hill skips unavailable crystal river", Sandbox, "spring_hill", []string{"spring_hill"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { InitRegistry(Prod) })
			InitRegistry(tt.env)
			if got := mustOffice(t, tt.office).AppointmentLookupOfficeIDs; !slices.Equal(got, tt.want) {
				t.Fatalf("AppointmentLookupOfficeIDs = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustOffice(t *testing.T, id string) *OfficeConfig {
	t.Helper()
	office, ok := LookupOfficeByID(id)
	if !ok {
		t.Fatalf("office %s is not in the selected registry", id)
	}
	return office
}

func TestCanonicalAppointmentTypeID(t *testing.T) {
	defer InitRegistry(Prod)

	InitRegistry(Prod)
	if got, ok := CanonicalAppointmentTypeID(1007); !ok || got != 1007 {
		t.Fatalf("prod CanonicalAppointmentTypeID(1007) = (%d, %v), want (1007, true)", got, ok)
	}
	if got, ok := CanonicalAppointmentTypeID(18); ok || got != 0 {
		t.Fatalf("prod CanonicalAppointmentTypeID(18) = (%d, %v), want (0, false)", got, ok)
	}

	InitRegistry(Sandbox)
	if got, ok := CanonicalAppointmentTypeID(18); !ok || got != 1007 {
		t.Fatalf("sandbox CanonicalAppointmentTypeID(18) = (%d, %v), want (1007, true)", got, ok)
	}
	if got, ok := CanonicalAppointmentTypeID(1010); ok || got != 0 {
		t.Fatalf("sandbox CanonicalAppointmentTypeID(1010) = (%d, %v), want (0, false)", got, ok)
	}
	if got, ok := CanonicalAppointmentTypeID(9999); ok || got != 0 {
		t.Fatalf("sandbox CanonicalAppointmentTypeID(9999) = (%d, %v), want (0, false)", got, ok)
	}
}

func TestSandboxRejectsUnmappedProductionIDs(t *testing.T) {
	defer InitRegistry(Prod)
	InitRegistry(Sandbox)
	if office, ok := LookupOffice("spring_hill"); !ok || office.FacilityID != "1032" {
		t.Fatal("sandbox office ID must resolve to the sandbox Spring Hill config")
	}
	for _, selector := range []string{"crystal_river", "+13523202007"} {
		if _, ok := LookupOffice(selector); ok {
			t.Errorf("sandbox unexpectedly accepts production office %q", selector)
		}
	}
	unmapped := []int{1010, 3364, 4244, 4245, 6167, 6168, 6169}
	for _, id := range unmapped {
		if _, ok := ResolveAppointmentTypeID(id); ok {
			t.Errorf("sandbox unexpectedly resolves unmapped production type %d", id)
		}
		if _, ok := CanonicalAppointmentTypeID(id); ok {
			t.Errorf("sandbox unexpectedly canonicalizes unmapped type %d", id)
		}
	}
	for canonical, sandbox := range map[int]int{1006: 12, 1004: 20, 1007: 18, 1005: 8, 1008: 1627} {
		if got, ok := ResolveAppointmentTypeID(canonical); !ok || got != sandbox {
			t.Errorf("sandbox medical mapping %d = (%d, %v), want %d", canonical, got, ok, sandbox)
		}
	}
	InitRegistry(Prod)
	for _, id := range unmapped {
		if got, ok := ResolveAppointmentTypeID(id); !ok || got != id {
			t.Errorf("production mapping changed for type %d", id)
		}
	}
	if office, ok := LookupOffice("crystal_river"); !ok || office.FacilityID != "1576" {
		t.Fatal("production Crystal River mapping changed")
	}
}

func TestOfficeConfig_AllowsAppointmentType(t *testing.T) {
	springHill := mustOffice(t, "spring_hill")
	crystalRiver := mustOffice(t, "crystal_river")
	hollywood := mustOffice(t, "hollywood")
	sweetwater := mustOffice(t, "sweetwater")

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
		{"north miami beach optical routine accepts vision type", mustOffice(t, "north_miami_beach_optical"), 1010, RoutingOpticalOnly, true},
		{"north miami beach optical rejects medical type", mustOffice(t, "north_miami_beach_optical"), 1006, RoutingAll, false},
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
			office:     mustOffice(t, "hollywood"),
			facilityID: "1480",
			columns:    []string{"1268", "1478", "1555", "1510", "1305"},
			optical:    []string{"1555", "1510", "1305"},
		},
		{
			name:       "sweetwater",
			office:     mustOffice(t, "sweetwater"),
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
				if _, ok := tt.office.Columns[columnID]; !ok {
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
	office := mustOffice(t, "north_miami_beach_optical")
	if office == nil {
		t.Fatal("expected North Miami Beach Optical office")
	}
	if office.FacilityID != "1582" {
		t.Fatalf("FacilityID = %q, want 1582", office.FacilityID)
	}
	if office.DefaultProfileID != "621" {
		t.Fatalf("DefaultProfileID = %q, want 621", office.DefaultProfileID)
	}
	if _, ok := office.Columns["1601"]; !ok {
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
	if col.ProfileID != "621" || col.DisplayName != "Dr. Miriam Bach" || col.SameStartCapacity != 0 {
		t.Fatalf("column 1601 = %+v, want Dr. Miriam Bach profile 621 single-booked", col)
	}
	if got := office.FriendlyProviderName("BACH, MIRIAM"); got != "Dr. Miriam Bach" {
		t.Fatalf("FriendlyProviderName(BACH, MIRIAM) = %q, want Dr. Miriam Bach", got)
	}
}

func TestOfficeConfig_SameStartCapacityScope(t *testing.T) {
	tests := []struct {
		name       string
		env        Environment
		office     string
		doubleBook []string
		singleBook []string
	}{
		{
			name:       "spring hill medical columns double-bookable",
			env:        Prod,
			office:     "spring_hill",
			doubleBook: []string{"1513", "1598", "1551", "1550"},
			singleBook: []string{"1600"},
		},
		{
			name:       "crystal river remains single-booked",
			env:        Prod,
			office:     "crystal_river",
			singleBook: []string{"1593"},
		},
		{
			name:       "sweetwater all columns have same-start capacity",
			env:        Prod,
			office:     "sweetwater",
			doubleBook: []string{"682", "1307", "1296", "1554", "1210"},
		},
		{
			name:       "hollywood all columns have same-start capacity",
			env:        Prod,
			office:     "hollywood",
			doubleBook: []string{"1268", "1478", "1555", "1510", "1305"},
		},
		{
			name:       "sandbox spring hill all columns double-bookable",
			env:        Sandbox,
			office:     "spring_hill",
			doubleBook: []string{"1716", "1723", "1726"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { InitRegistry(Prod) })
			InitRegistry(tt.env)
			office := mustOffice(t, tt.office)
			for _, columnID := range tt.doubleBook {
				col, ok := office.Columns[columnID]
				if !ok {
					t.Fatalf("missing column %s", columnID)
				}
				if col.SameStartCapacity != 2 {
					t.Fatalf("column %s SameStartCapacity = %d, want 2", columnID, col.SameStartCapacity)
				}
			}
			for _, columnID := range tt.singleBook {
				col, ok := office.Columns[columnID]
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
			office:     mustOffice(t, "sweetwater"),
			routineIDs: []string{"1296", "1554", "1210"},
			medicalIDs: []string{"682", "1307"},
		},
		{
			name:       "hollywood",
			office:     mustOffice(t, "hollywood"),
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
	office := mustOffice(t, "sweetwater")
	if got := office.ProvidersForRoutingAndDOB(RoutingOpticalOnly, "", testNow); len(got) != 3 {
		t.Fatalf("missing DOB routine providers = %v, want all without an age filter", got)
	}
	if got := office.ProvidersForRoutingAndDOB(RoutingOpticalOnly, "not-a-date", testNow); len(got) != 0 {
		t.Fatalf("invalid DOB routine providers = %v, want none", got)
	}
	if !office.ColumnAllowsDOB("682", "", testNow) {
		t.Fatal("Bach column should allow missing DOB because it has no minimum age")
	}
	if office.ColumnAllowsDOB("1296", "", testNow) {
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
			dob := testNow.AddDate(-tt.age, 0, 0).Format("01/02/2006")
			got := office.ProvidersForRoutingAndDOB(RoutingOpticalOnly, dob, testNow)
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
	defer InitRegistry(Prod)

	InitRegistry(Sandbox)
	office, ok := LookupOffice("+14843989071")
	if !ok {
		t.Fatal("sandbox registry should have +14843989071")
	}
	if office.FacilityID != "1032" {
		t.Errorf("sandbox FacilityID = %q, want %q", office.FacilityID, "1032")
	}
	if _, ok := office.Columns["1716"]; !ok {
		t.Error("sandbox registry should have column 1716 (Bach)")
	}
	if _, ok := office.Columns["1513"]; ok {
		t.Error("sandbox registry should NOT have prod column 1513")
	}

	_, ok = LookupOffice("+17275919997")
	if ok {
		t.Error("sandbox registry should NOT have prod phone +17275919997")
	}

	sandboxDefault := DefaultOffice()
	if sandboxDefault == nil {
		t.Fatal("DefaultOffice() returned nil in sandbox")
	}
	if sandboxDefault.FacilityID != "1032" {
		t.Errorf("sandbox DefaultOffice().FacilityID = %q, want %q", sandboxDefault.FacilityID, "1032")
	}

	InitRegistry(Prod)
	office = DefaultOffice()
	if office.FacilityID != "1568" {
		t.Errorf("prod FacilityID = %q, want %q", office.FacilityID, "1568")
	}
	if _, ok := office.Columns["1513"]; !ok {
		t.Error("prod registry should have column 1513 (Bach)")
	}
	if _, ok := office.Columns["1600"]; !ok {
		t.Error("prod registry should have column 1600 (Dr. Melissa Otero)")
	}
	if _, ok := office.Columns["1716"]; ok {
		t.Error("prod registry should NOT have sandbox column 1716")
	}

}

func TestParseEnvironmentAcceptsOnlyProdAndSandbox(t *testing.T) {
	for _, value := range []string{"prod", "sandbox"} {
		if env, err := ParseEnvironment(value); err != nil || string(env) != value {
			t.Errorf("ParseEnvironment(%q) = (%q, %v), want (%q, nil)", value, env, err, value)
		}
	}
	for _, value := range []string{"", "dev", "production", "Prod", "SANDBOX", " prod", "prod "} {
		if env, err := ParseEnvironment(value); err == nil {
			t.Errorf("ParseEnvironment(%q) = %q, want an error", value, env)
		}
	}
}

func TestInitRegistryRejectsAnUnknownEnvironment(t *testing.T) {
	t.Cleanup(func() { InitRegistry(Prod) })
	for _, env := range []Environment{"", "dev"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("InitRegistry(%q) selected a registry, want a panic", env)
				}
			}()
			InitRegistry(env)
		}()
	}
}

func TestEveryOfficeColumnHasDisplayName(t *testing.T) {
	t.Cleanup(func() { InitRegistry(Prod) })
	for _, env := range []Environment{Prod, Sandbox} {
		InitRegistry(env)
		for _, id := range OfficeIDs() {
			office, ok := LookupOfficeByID(id)
			if !ok {
				continue
			}
			for columnID, column := range office.Columns {
				if column.DisplayName == "" {
					t.Errorf("%s office %s column %s has no display name", env, id, columnID)
				}
			}
		}
	}
}
