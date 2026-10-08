package insurance

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden decision files in testdata")

var callerPhrasings = []string{
	"", "health plan", "cash", "self pay",
	"it's through my employer", "commercial", "the first one", "military", "I'm a student",
	"I'm not sure", "Clover Medicare", "Medicare please", "the Medicare one I think",
	"Medicare red white and blue card",
	"Aetna", "Aetna Medicar", "Humana", "Humana Gold PPO", "Humana from work",
	"Florida Blue", "Florida Blue Medicare Advantage", "Blue Cross", "BCBS", "BCBS HMO",
	"United", "UHC", "United Healthcare PPO", "Cigna", "Molina", "Molina Medicade", "Simply",
	"Staywell Medicaid", "Tier 1",
	"Medicare", "Medicaid", "Medicade", "Medicare Advantage", "Medicare Part B",
	"Medicare Health Insurance", "Original Medicare", "Traditional Medicare", "Regular Medicare",
	"Medicare Supplement", "iCare", "I care", "I-Care",
	"PPO", "HMO", "that one", "the second one", "the EPO one", "the Health one",
}

func TestDecisionsMatchGolden(t *testing.T) {
	checkGolden(t, "testdata/phrasings.golden.tsv", phrasingRows(t))
	checkGolden(t, "testdata/plans.golden.tsv", planRows(t))
}

func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/insurance -run Golden -update)", err)
	}
	if got == string(want) {
		return
	}
	wantRows := strings.Split(string(want), "\n")
	gotRows := strings.Split(got, "\n")
	diffs := 0
	for i := range max(len(wantRows), len(gotRows)) {
		w, g := row(wantRows, i), row(gotRows, i)
		if w != g {
			t.Errorf("%s row %d\nwant %s\n got %s", filepath.Base(path), i+1, w, g)
			if diffs++; diffs == 20 {
				break
			}
		}
	}
	t.Fatalf("decisions changed; if intended, run go test ./internal/insurance -run Golden -update and review the diff of %s", filepath.Base(path))
}

func row(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

func phrasingRows(t *testing.T) string {
	var b strings.Builder
	b.WriteString("office\tcoverage\theard\toutcome\tplanId\tcarrierId\toptions\treason\tcallerNotice\tanswer\n")
	forEachOfficeCoverage(t, func(o *domain.OfficeConfig, coverage string) {
		for _, heard := range callerPhrasings {
			d := DecideInsurance(heard, coverage, o, adultDOB)
			options := []string{}
			for _, option := range d.Options {
				options = append(options, option.PlanID)
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", o.ID, coverage, heard, d.Outcome, d.PlanID, d.CarrierID, strings.Join(options, ","), d.Reason, d.CallerNotice, d.Answer)
		}
	})
	return b.String()
}

func planRows(t *testing.T) string {
	var b strings.Builder
	b.WriteString("office\tcoverage\tplanId\toutcome\tcarrierId\trequirements\tadultDoctors\tchildDoctors\treason\tcallerNotice\tanswer\n")
	forEachOfficeCoverage(t, func(o *domain.OfficeConfig, coverage string) {
		for _, p := range listForOffice(o.ID).Plans {
			if p.Coverage != coverage {
				continue
			}
			adult := DecidePlan(p.ID, coverage, o, adultDOB)
			child := DecidePlan(p.ID, coverage, o, dobYearsAgo(8))
			kinds := []string{}
			for _, r := range adult.Requirements {
				kinds = append(kinds, r.Kind)
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", o.ID, coverage, p.ID, adult.Outcome, adult.CarrierID, strings.Join(kinds, ","), strings.Join(adult.AllowedProviders, ","), strings.Join(child.AllowedProviders, ","), adult.Reason, adult.CallerNotice, adult.Answer)
		}
	})
	return b.String()
}

func forEachOfficeCoverage(t *testing.T, visit func(o *domain.OfficeConfig, coverage string)) {
	officeIDs := domain.OfficeIDs()
	slices.Sort(officeIDs)
	for _, officeID := range officeIDs {
		o := office(t, officeID)
		for _, coverage := range []string{"medical", "routine_vision"} {
			if officeSupports(o, coverage) {
				visit(o, coverage)
			}
		}
	}
}
