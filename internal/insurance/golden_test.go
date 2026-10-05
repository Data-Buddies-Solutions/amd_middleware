package insurance

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/decisions.golden.tsv")

const goldenPath = "testdata/decisions.golden.tsv"

var callerPhrasings = []string{
	"", "health plan", "cash", "self pay",
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
	got := goldenRows(t)
	if *updateGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
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
			t.Errorf("row %d\nwant %s\n got %s", i+1, w, g)
			if diffs++; diffs == 20 {
				break
			}
		}
	}
	t.Fatalf("decisions changed; if intended, run go test ./internal/insurance -run Golden -update and review the diff of %s", filepath.Base(goldenPath))
}

func row(rows []string, i int) string {
	if i < len(rows) {
		return rows[i]
	}
	return ""
}

func goldenRows(t *testing.T) string {
	var b strings.Builder
	b.WriteString("office\tcoverage\theard\toutcome\tplanId\tcarrierId\toptions\tanswer\n")
	for _, list := range catalog {
		for _, officeID := range list.Offices {
			o := office(t, officeID)
			for _, coverage := range []string{"medical", "routine_vision"} {
				if !officeSupports(o, coverage) {
					continue
				}
				for _, heard := range goldenPhrasings(list, coverage) {
					d := DecideInsurance(heard, coverage, o, adultDOB)
					options := []string{}
					for _, option := range d.Options {
						options = append(options, option.PlanID)
					}
					fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", officeID, coverage, heard, d.Outcome, d.PlanID, d.CarrierID, strings.Join(options, ","), d.Answer)
				}
			}
		}
	}
	return b.String()
}

func goldenPhrasings(list planList, coverage string) []string {
	phrasings := slices.Clone(callerPhrasings)
	for _, p := range list.Plans {
		if p.Coverage == coverage {
			phrasings = append(phrasings, planNames(p)...)
		}
	}
	seen := map[string]bool{}
	return slices.DeleteFunc(phrasings, func(s string) bool {
		key := strings.ToLower(s)
		defer func() { seen[key] = true }()
		return seen[key]
	})
}
