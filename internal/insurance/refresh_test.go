package insurance

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/safeerrors"
)

const removedPlan = "aetna-better-health-medicaid-medical"

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
	installRevision(t, "rev-a", RunningCatalog())
	if !removedPlanSchedules(t) || active.Load().id != "rev-a" {
		t.Fatalf("installed revision %q does not decide revision A", active.Load().id)
	}
	if !strings.Contains(logs.String(), "insurance_catalog_installed revision=rev-a previous_revision=none") {
		t.Fatalf("logs = %q", logs)
	}
}

func TestRefreshSwapsToAChangedRevision(t *testing.T) {
	a := RunningCatalog()
	installRevision(t, "rev-a", a)
	logs := captureLog(t)
	if err := Refresh(context.Background(), serve("rev-b", catalogJSON(t, withoutPlan(t, a, removedPlan)), nil)); err != nil {
		t.Fatal(err)
	}
	if removedPlanSchedules(t) || active.Load().id != "rev-b" {
		t.Fatalf("revision %q still decides the plan revision B removed", active.Load().id)
	}
	if strings.Contains(string(catalogJSON(t, RunningCatalog())), removedPlan) {
		t.Fatal("the export still has the plan revision B removed")
	}
	if !strings.Contains(logs.String(), "insurance_catalog_installed revision=rev-b previous_revision=rev-a") {
		t.Fatalf("logs = %q", logs)
	}
}

func TestRefreshIgnoresTheSameRevisionID(t *testing.T) {
	installRevision(t, "rev-a", RunningCatalog())
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
	a := RunningCatalog()
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
