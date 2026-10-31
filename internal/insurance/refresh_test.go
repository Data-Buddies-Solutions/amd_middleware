package insurance

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/safeerrors"
)

const removedPlan = "aetna-better-health-medicaid-medical"

func fixtureCatalog(t *testing.T) InsuranceCatalog {
	t.Helper()
	b, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var c InsuranceCatalog
	if err := decodeStrictJSON(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func catalogJSON(t *testing.T, c InsuranceCatalog) []byte {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func withoutPlan(t *testing.T, c InsuranceCatalog, planID string) InsuranceCatalog {
	t.Helper()
	var copied InsuranceCatalog
	if err := json.Unmarshal(catalogJSON(t, c), &copied); err != nil {
		t.Fatal(err)
	}
	for i := range copied.Regions {
		region := &copied.Regions[i]
		region.Plans = slices.DeleteFunc(region.Plans, func(p CatalogPlan) bool { return p.ID == planID })
		for j := range region.Tables {
			region.Tables[j].Rows = slices.DeleteFunc(region.Tables[j].Rows, func(r CatalogRow) bool { return r.PlanID == planID })
		}
	}
	return copied
}

func serve(id string, body []byte, err error) CatalogReader {
	return func(context.Context) (string, []byte, error) { return id, body, err }
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &b
}

func installRevision(t *testing.T, id string, c InsuranceCatalog) {
	t.Helper()
	previous := active.Load()
	t.Cleanup(func() { active.Store(previous) })
	active.Store(nil)
	if err := Refresh(context.Background(), serve(id, catalogJSON(t, c), nil)); err != nil {
		t.Fatal(err)
	}
}

func removedPlanSchedules(t *testing.T) bool {
	t.Helper()
	return DecidePlan(removedPlan, "medical", office(t, "sweetwater"), adultDOB, testNow).CanSchedule
}

func TestRefreshInstallsTheFirstRevision(t *testing.T) {
	logs := captureLog(t)
	installRevision(t, "rev-a", fixtureCatalog(t))
	if !removedPlanSchedules(t) || active.Load().id != "rev-a" {
		t.Fatalf("installed revision %q does not decide revision A", active.Load().id)
	}
	if !strings.Contains(logs.String(), "insurance_catalog_installed revision=rev-a previous_revision=none") {
		t.Fatalf("logs = %q", logs)
	}
}

func TestRefreshSwapsToAChangedRevision(t *testing.T) {
	a := fixtureCatalog(t)
	installRevision(t, "rev-a", a)
	logs := captureLog(t)
	if err := Refresh(context.Background(), serve("rev-b", catalogJSON(t, withoutPlan(t, a, removedPlan)), nil)); err != nil {
		t.Fatal(err)
	}
	if removedPlanSchedules(t) || active.Load().id != "rev-b" {
		t.Fatalf("revision %q still decides the plan revision B removed", active.Load().id)
	}
	if !strings.Contains(logs.String(), "insurance_catalog_installed revision=rev-b previous_revision=rev-a") {
		t.Fatalf("logs = %q", logs)
	}
}

func TestRefreshIgnoresTheSameRevisionID(t *testing.T) {
	installRevision(t, "rev-a", fixtureCatalog(t))
	installed := active.Load()
	logs := captureLog(t)
	if err := Refresh(context.Background(), serve("rev-a", []byte("not even JSON"), nil)); err != nil {
		t.Fatalf("an unchanged revision was parsed again: %v", err)
	}
	if active.Load() != installed || logs.Len() != 0 {
		t.Fatalf("unchanged revision swapped or logged: %q", logs)
	}
}

func TestRefreshKeepsServingTheCurrentRevisionOnFailure(t *testing.T) {
	a := fixtureCatalog(t)
	invalid := withoutPlan(t, a, removedPlan)
	invalid.Regions[0].Plans[0].Family = ""
	unknownField := bytes.Replace(catalogJSON(t, a), []byte(`"selfPay":false,`), []byte(`"selfPay":false,"extra":1,`), 1)
	tests := []struct {
		name     string
		read     CatalogReader
		category safeerrors.Category
		cause    string
	}{
		{"product unreachable", serve("", nil, safeerrors.New(safeerrors.CategoryNetwork, "POST /v1/agent/insurance/read: network")), safeerrors.CategoryNetwork, "network"},
		{"product timed out", serve("", nil, safeerrors.New(safeerrors.CategoryTimeout, "POST /v1/agent/insurance/read: timeout")), safeerrors.CategoryTimeout, "timeout"},
		{"not JSON", serve("rev-b", []byte(`{"carriers":`), nil), safeerrors.CategoryInvalidResponse, "revision rev-b is not an InsuranceCatalog"},
		{"unknown field", serve("rev-b", unknownField, nil), safeerrors.CategoryInvalidResponse, `unknown field \"extra\"`},
		{"fails validation", serve("rev-b", catalogJSON(t, invalid), nil), safeerrors.CategoryInvalidResponse, "breaks 1 catalog rules, first invalid_family"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installRevision(t, "rev-a", a)
			installed := active.Load()
			logs := captureLog(t)
			err := Refresh(context.Background(), tc.read)
			if safeerrors.Classify(err) != tc.category {
				t.Fatalf("err = %v (%s), want %s", err, safeerrors.Classify(err), tc.category)
			}
			if active.Load() != installed || !removedPlanSchedules(t) {
				t.Fatal("a failed refresh changed the installed revision")
			}
			line := logs.String()
			if !strings.Contains(line, "insurance_catalog_refresh_failed current_revision=rev-a category="+string(tc.category)) || !strings.Contains(line, tc.cause) {
				t.Fatalf("log = %q, want the failure, current revision, and %q", line, tc.cause)
			}
		})
	}
}

func TestRefreshWithNothingInstalledLeavesNothingInstalled(t *testing.T) {
	previous := active.Load()
	t.Cleanup(func() { active.Store(previous) })
	active.Store(nil)
	logs := captureLog(t)
	if err := Refresh(context.Background(), serve("", nil, safeerrors.New(safeerrors.CategoryNetwork, "POST /v1/agent/insurance/read: network"))); err == nil {
		t.Fatal("Refresh succeeded without a revision")
	}
	if active.Load() != nil || !strings.Contains(logs.String(), "insurance_catalog_refresh_failed current_revision=none") {
		t.Fatalf("installed=%v logs=%q", active.Load(), logs)
	}
}

func TestRefreshInstallsARevisionWhoseDoctorColumnsDifferFromTheOfficeTable(t *testing.T) {
	const planID, dropped, unknown = "aetna-better-health", "Dr. Joseph Licht", "Dr. Nobody"
	a := fixtureCatalog(t)
	installRevision(t, "rev-a", a)
	springHill := office(t, "spring_hill")
	coverage := ""
	for _, region := range a.Regions {
		for _, p := range region.Plans {
			if p.ID == planID && region.ID == "spring_hill_crystal_river" {
				coverage = p.Coverage
			}
		}
	}
	before := DecidePlan(planID, coverage, springHill, adultDOB, testNow).AllowedProviders

	var drifted InsuranceCatalog
	if err := json.Unmarshal(catalogJSON(t, a), &drifted); err != nil {
		t.Fatal(err)
	}
	for i := range drifted.Regions {
		for j := range drifted.Regions[i].Tables {
			table := &drifted.Regions[i].Tables[j]
			if !slices.Equal(table.Offices, []string{"spring_hill"}) {
				continue
			}
			table.Doctors = append(slices.DeleteFunc(table.Doctors, func(d string) bool { return d == dropped }), unknown)
			for _, row := range table.Rows {
				delete(row.Doctors, dropped)
				if row.PlanID == planID {
					row.Doctors[unknown] = "yes"
				}
			}
		}
	}

	rules := []CatalogRule{}
	for _, e := range ValidateCatalog(drifted) {
		rules = append(rules, e.Rule)
	}
	if !slices.Equal(rules, []CatalogRule{ruleUnknownDoctorColumn, ruleMissingDoctorColumn}) {
		t.Fatalf("validate rules = %v, want the drift still rejected", rules)
	}

	logs := captureLog(t)
	if err := Refresh(context.Background(), serve("rev-b", catalogJSON(t, drifted), nil)); err != nil {
		t.Fatalf("refresh refused a revision whose only problem is doctor columns: %v", err)
	}
	if active.Load().id != "rev-b" {
		t.Fatalf("installed revision = %q, want rev-b", active.Load().id)
	}
	line := logs.String()
	if !strings.Contains(line, "insurance_catalog_doctor_columns_differ revision=rev-b count=2") ||
		!strings.Contains(line, "insurance_catalog_installed revision=rev-b previous_revision=rev-a") {
		t.Fatalf("logs = %q", line)
	}

	after := DecidePlan(planID, coverage, springHill, adultDOB, testNow).AllowedProviders
	removed := []string{}
	for _, doctor := range before {
		if !slices.Contains(after, doctor) {
			removed = append(removed, displayName(springHill, doctor))
		}
	}
	if len(after) != len(before)-1 || !slices.Equal(removed, []string{dropped}) {
		t.Fatalf("allowed providers before=%v after=%v, want only %s removed", before, after, dropped)
	}
}
