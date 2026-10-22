package domain

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var updateRegistryGolden = flag.Bool("update", false, "rewrite the golden registry files in testdata")

var goldenNow = time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)

var goldenOfficeIDs = []string{"spring_hill", "crystal_river", "hollywood", "sweetwater", "north_miami_beach_optical", "atlantis"}

var goldenRoutings = []RoutingRule{RoutingNotAccepted, RoutingBachOnly, RoutingBachLicht, RoutingAll, RoutingOpticalOnly}

var goldenTypeIDs = []int{-1, 0, 8, 12, 18, 20, 1004, 1005, 1006, 1007, 1008, 1010, 1627, 3364, 4244, 4245, 6167, 6168, 6169, 9999}

var goldenLookups = []string{
	"+17275919997", "17275919997", "7275919997", "(727) 591-9997", " +17275919997 ",
	"+13523202007", "+19542872010", "19542872010", "9542872010", "(954) 287-2010",
	"+17864657475", "+17864654845", "+17866134310", "+17864657479", "+17864654836", "+17864654882",
	"+13055095333", "3055095333", "+14843989071", "4843989071", "+15551234567", "",
	"spring_hill", "Spring Hill", "spring hill", "SPRING-HILL", "springhill",
	"crystal_river", "Crystal River", "hollywood", "Hollywood", "sweetwater", "Sweetwater",
	"north_miami_beach_optical", "North Miami Beach Optical", "north miami beach", "atlantis",
}

var goldenProviderNames = []string{
	"BACH, AUSTIN", "BACH, MIRIAM", "LICHT, JOSEPH", "NOEL, DR", "OTERO, MELISSA", "CASAS, MARIA",
	"FARNAN, KYLER", "CALERO, GISSELLE", "VIDAL, LISBET", "UNKNOWN, DOCTOR", "",
}

type goldenDOB struct {
	label string
	dob   string
}

func goldenDOBs() []goldenDOB {
	dobs := []goldenDOB{{"none", ""}, {"invalid", "not-a-date"}}
	for _, age := range []int{3, 4, 5, 6, 7, 10, 17, 18, 40} {
		dobs = append(dobs, goldenDOB{fmt.Sprintf("age%d", age), goldenNow.AddDate(-age, 0, -1).Format("01/02/2006")})
	}
	return dobs
}

func TestRegistryMatchesGolden(t *testing.T) {
	for _, env := range []Environment{Prod, Sandbox} {
		t.Run(string(env), func(t *testing.T) {
			t.Cleanup(func() { InitRegistry(Prod) })
			InitRegistry(env)
			checkRegistryGolden(t, filepath.Join("testdata", "registry."+string(env)+".golden"), registryRows())
		})
	}
}

func checkRegistryGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateRegistryGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/domain -run Golden -update)", err)
	}
	if got == string(want) {
		return
	}
	wantRows := strings.Split(string(want), "\n")
	gotRows := strings.Split(got, "\n")
	diffs := 0
	for i := range max(len(wantRows), len(gotRows)) {
		w, g := goldenRow(wantRows, i), goldenRow(gotRows, i)
		if w != g {
			t.Errorf("%s row %d\nwant %s\n got %s", filepath.Base(path), i+1, w, g)
			if diffs++; diffs == 20 {
				break
			}
		}
	}
	t.Fatalf("registry behavior changed; if intended, run go test ./internal/domain -run Golden -update and review the diff of %s", filepath.Base(path))
}

func goldenRow(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

func registryRows() string {
	var b strings.Builder
	fmt.Fprintf(&b, "default\t%s\n", DefaultOffice().ID)
	fmt.Fprintf(&b, "officeIDs\t%s\n", strings.Join(TableOfficeIDs(), ","))
	for _, phone := range slices.Sorted(maps.Keys(selected().byPhone)) {
		fmt.Fprintf(&b, "phone\t%s\t%s\n", phone, selected().byPhone[phone].ID)
	}
	for _, input := range goldenLookups {
		office, ok := LookupOffice(input)
		fmt.Fprintf(&b, "lookup\t%q\t%s\n", input, goldenOfficeID(office, ok))
	}
	for _, input := range []string{"", "Hollywood", "atlantis"} {
		office, err := ResolveOffice(input)
		if err != nil {
			fmt.Fprintf(&b, "resolve\t%q\terror\t%s\n", input, err)
			continue
		}
		fmt.Fprintf(&b, "resolve\t%q\t%s\n", input, office.ID)
	}
	for _, id := range goldenOfficeIDs {
		office, ok := LookupOfficeByID(id)
		if !ok {
			fmt.Fprintf(&b, "office\t%s\tnot found\n", id)
			continue
		}
		writeGoldenOffice(&b, office)
	}
	writeGoldenAppointmentTypes(&b)
	return b.String()
}

func goldenOfficeID(office *OfficeConfig, ok bool) string {
	if !ok {
		return "not found"
	}
	return office.ID
}

func writeGoldenOffice(b *strings.Builder, office *OfficeConfig) {
	fmt.Fprintf(b, "office\t%s\tdisplay=%s\tfacility=%s\tdefaultProfile=%s\tpediatric=%s\tsupportsMedical=%t\tlookupGroup=%s\n",
		office.ID, office.DisplayName, office.FacilityID, office.DefaultProfileID, office.PediatricRouting,
		office.SupportsMedical(), strings.Join(office.AppointmentLookupOfficeIDs, ","))
	columnIDs := goldenSortedKeys(office.Columns)
	for _, id := range columnIDs {
		c := office.Columns[id]
		windows := []string{}
		for _, w := range c.SameStartWindows {
			windows = append(windows, fmt.Sprintf("%s %02d:%02d-%02d:%02d", w.Weekday.String()[:3], w.StartMinute/60, w.StartMinute%60, w.EndMinute/60, w.EndMinute%60))
		}
		fmt.Fprintf(b, "column\t%s\t%s\tprofile=%s\tdisplay=%s\tshort=%s\tmatch=%s\tminAge=%d\tcapacity=%d\twindows=%s\n",
			office.ID, id, c.ProfileID, c.DisplayName, c.ShortName, c.MatchKey, c.MinAgeYears, c.SameStartCapacity, strings.Join(windows, ","))
		allows := []string{}
		for _, d := range goldenDOBs() {
			allows = append(allows, fmt.Sprintf("%s=%t", d.label, office.ColumnAllowsDOB(id, d.dob, goldenNow)))
		}
		fmt.Fprintf(b, "columnDOB\t%s\t%s\t%s\n", office.ID, id, strings.Join(allows, " "))
	}
	adult := goldenNow.AddDate(-40, 0, 0).Format("01/02/2006")
	child := goldenNow.AddDate(-10, 0, 0).Format("01/02/2006")
	for _, rule := range goldenRoutings {
		columns := office.ColumnsForRouting(rule)
		columnList := "nil"
		if columns != nil {
			columnList = "[" + strings.Join(goldenSortedKeys(columns), ",") + "]"
		}
		providers := office.ProvidersForRouting(rule)
		providerList := "nil"
		if providers != nil {
			providerList = "[" + strings.Join(providers, ",") + "]"
		}
		fmt.Fprintf(b, "routing\t%s\t%s\tcolumns=%s\tproviders=%s\tsupports=%t\tadult=%s\tminor=%s\n",
			office.ID, rule, columnList, providerList, office.SupportsRouting(rule),
			office.SchedulingRouting(rule, adult, goldenNow), office.SchedulingRouting(rule, child, goldenNow))
		for _, d := range goldenDOBs() {
			byDOB := office.ColumnsForRoutingAndDOB(rule, d.dob, goldenNow)
			byDOBList := "nil"
			if byDOB != nil {
				byDOBList = "[" + strings.Join(goldenSortedKeys(byDOB), ",") + "]"
			}
			names := office.ProvidersForRoutingAndDOB(rule, d.dob, goldenNow)
			nameList := "nil"
			if names != nil {
				nameList = "[" + strings.Join(names, ",") + "]"
			}
			fmt.Fprintf(b, "routingDOB\t%s\t%s\t%s\tcolumns=%s\tproviders=%s\n", office.ID, rule, d.label, byDOBList, nameList)
		}
		allowed := []string{}
		for _, id := range goldenTypeIDs {
			if office.AllowsAppointmentType(id, rule) {
				allowed = append(allowed, fmt.Sprint(id))
			}
		}
		fmt.Fprintf(b, "allowsTypes\t%s\t%s\t%s\n", office.ID, rule, strings.Join(allowed, ","))
	}
	for _, id := range goldenTypeIDs {
		color, colorOK := office.AppointmentColor(id)
		name, nameOK := office.AppointmentTypeName(id)
		fmt.Fprintf(b, "typeLabel\t%s\t%d\tcolor=%s/%t\tname=%s/%t\n", office.ID, id, color, colorOK, name, nameOK)
	}
	for _, name := range goldenProviderNames {
		fmt.Fprintf(b, "friendly\t%s\t%q\t%q\n", office.ID, name, office.FriendlyProviderName(name))
	}
}

func writeGoldenAppointmentTypes(b *strings.Builder) {
	fmt.Fprintf(b, "validTypes\t%s\n", ValidAppointmentTypeIDs())
	fmt.Fprintf(b, "typeCount\t%d\n", len(AppointmentTypes))
	for _, id := range goldenTypeIDs {
		appointmentType, ok := AppointmentTypes[id]
		resolved, resolvedOK := ResolveAppointmentTypeID(id)
		canonical, canonicalOK := CanonicalAppointmentTypeID(id)
		fmt.Fprintf(b, "type\t%d\tknown=%t\tname=%s\tcolor=%s\tvisit=%s\tage=%s\tvisitType=%s\tresolve=%d/%t\tcanonical=%d/%t\n",
			id, ok, appointmentType.Name, appointmentType.Color, appointmentType.VisitType, appointmentType.AgeBand,
			AppointmentVisitType(id), resolved, resolvedOK, canonical, canonicalOK)
	}
}

func goldenSortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
