package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/insurance/insurancetest"
	"advancedmd-token-management/internal/safelog"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
)

const (
	refreshSchedulerToken = "scheduler-id-token"
	productInsuranceToken = "product-insurance-token"
	removedPlan           = "aetna-better-health-medicaid-medical"
)

type fakeProduct struct {
	mu         sync.Mutex
	revisionID string
	catalog    []byte
	status     int
	hang       chan struct{}
	reads      int
	server     *httptest.Server
}

func newFakeProduct(t *testing.T) *fakeProduct {
	t.Helper()
	product := &fakeProduct{status: http.StatusOK}
	product.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		product.mu.Lock()
		product.reads++
		status, id, catalog, hang := product.status, product.revisionID, product.catalog, product.hang
		product.mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/v1/agent/insurance/read" || r.Header.Get("Authorization") != "Bearer "+productInsuranceToken {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if hang != nil {
			select {
			case <-hang:
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			w.Write([]byte(`{"revisionId":"` + id + `","catalog":` + string(catalog) + `}`))
		}
	}))
	t.Cleanup(product.server.Close)
	return product
}

func (p *fakeProduct) publish(t *testing.T, id string, c insurance.InsuranceCatalog) {
	t.Helper()
	p.publishBytes(id, marshal(t, c))
}

func (p *fakeProduct) publishBytes(id string, catalog []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revisionID, p.catalog, p.status = id, catalog, http.StatusOK
}

func (p *fakeProduct) fail(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = status
}

func (p *fakeProduct) hangUntil(release chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hang = release
}

func (p *fakeProduct) readCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

type refreshRig struct {
	router  http.Handler
	product *fakeProduct
	records *advancedmdtest.Adapter
}

func newRefreshRig(t *testing.T) *refreshRig {
	t.Helper()
	t.Cleanup(insurancetest.Install)
	product := newFakeProduct(t)
	reader := clients.NewProductClient(product.server.Client(), product.server.URL, productInsuranceToken)
	records := advancedmdtest.NewAdapter()
	scheduler := schedulingmodule.New(records, "test-scheduling-secret", testNow)
	authorizer := newMaintenanceAuthorizer(
		"https://middleware.example.test",
		"middleware-maintenance@example.iam.gserviceaccount.com",
		oidcValidatorFunc(func(_ context.Context, token, _ string) (oidcIdentity, error) {
			if token != refreshSchedulerToken {
				return oidcIdentity{}, errors.New("invalid token")
			}
			return oidcIdentity{Issuer: googleServiceAccountIssuer, Email: "middleware-maintenance@example.iam.gserviceaccount.com", EmailVerified: true}, nil
		}),
	)
	router := NewRouter(NewHandlers(nil, nil, scheduler, reader.ReadInsuranceCatalog, testNow), "test-secret", authorizer)
	return &refreshRig{router: router, product: product, records: records}
}

func (rig *refreshRig) refresh(t *testing.T, ctx context.Context, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ops/insurance/refresh", nil).WithContext(ctx)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	return w
}

func (rig *refreshRig) install(t *testing.T, id string, c insurance.InsuranceCatalog) {
	t.Helper()
	rig.product.publish(t, id, c)
	if w := rig.refresh(t, context.Background(), refreshSchedulerToken); w.Code != http.StatusNoContent {
		t.Fatalf("refresh to %s: status=%d body=%s", id, w.Code, w.Body)
	}
}

func (rig *refreshRig) plans(t *testing.T, office string) insurance.PlanList {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/insurance/plans?office="+office+"&coverage=medical", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	var list insurance.PlanList
	decoder := json.NewDecoder(w.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&list); err != nil || w.Code != http.StatusOK {
		t.Errorf("plans status=%d err=%v", w.Code, err)
	}
	return list
}

func (rig *refreshRig) slotsOutcome(t *testing.T, planID string) string {
	t.Helper()
	body := `{"office":"Sweetwater","startDate":"2026-06-03","dob":"01/15/1980","visitType":"medical","insurancePlanId":"` + planID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/slots", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	var got struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("slots body = %s", w.Body)
	}
	return got.Outcome
}

func withoutPlan(t *testing.T, c insurance.InsuranceCatalog, planID string) insurance.InsuranceCatalog {
	t.Helper()
	var copied insurance.InsuranceCatalog
	if err := json.Unmarshal(marshal(t, c), &copied); err != nil {
		t.Fatal(err)
	}
	for i := range copied.Regions {
		region := &copied.Regions[i]
		region.Plans = slices.DeleteFunc(region.Plans, func(p insurance.CatalogPlan) bool { return p.ID == planID })
		for j := range region.Tables {
			region.Tables[j].Rows = slices.DeleteFunc(region.Tables[j].Rows, func(r insurance.CatalogRow) bool { return r.PlanID == planID })
		}
	}
	return copied
}

func listedPlan(list insurance.PlanList, planID string) (insurance.PlanSummary, bool) {
	for _, p := range list.Plans {
		if p.PlanID == planID {
			return p, true
		}
	}
	return insurance.PlanSummary{}, false
}

func TestInsuranceRefreshRequiresTheSchedulerIdentity(t *testing.T) {
	rig := newRefreshRig(t)
	for _, bearer := range []string{"", "test-secret", "forged"} {
		if w := rig.refresh(t, context.Background(), bearer); w.Code != http.StatusUnauthorized {
			t.Fatalf("bearer %q: status=%d", bearer, w.Code)
		}
	}
	if rig.product.readCount() != 0 {
		t.Fatalf("an unauthorized refresh read Product %d times", rig.product.readCount())
	}
}

func TestInsuranceRefreshServesEachPublishedRevision(t *testing.T) {
	rig := newRefreshRig(t)
	a := insurancetest.Catalog()
	rig.install(t, "rev-a", a)

	listA := rig.plans(t, "hollywood")
	removed, listed := listedPlan(listA, removedPlan)
	if listA.CatalogRevisionID != "rev-a" || !listed || !slices.Contains(removed.AcceptedAt, "Sweetwater") {
		t.Fatalf("revision A plan list = %s with %s listed=%v %+v", listA.CatalogRevisionID, removedPlan, listed, removed)
	}
	if outcome := rig.slotsOutcome(t, removedPlan); outcome == schedulingmodule.AvailabilityOutcomeInsuranceNotCleared || rig.records.SchedulerSetupCalls != 1 {
		t.Fatalf("revision A slots outcome = %q, setup reads = %d; want the plan cleared and inventory read", outcome, rig.records.SchedulerSetupCalls)
	}

	logs := captureLogs(func() { rig.install(t, "rev-b", withoutPlan(t, a, removedPlan)) })

	listB := rig.plans(t, "hollywood")
	if _, listed := listedPlan(listB, removedPlan); listB.CatalogRevisionID != "rev-b" || listed || len(listB.Plans) != len(listA.Plans)-1 {
		t.Fatalf("revision B plan list = %s with %d plans, %s listed=%v", listB.CatalogRevisionID, len(listB.Plans), removedPlan, listed)
	}
	if outcome := rig.slotsOutcome(t, removedPlan); outcome != schedulingmodule.AvailabilityOutcomeInsuranceNotCleared || rig.records.SchedulerSetupCalls != 1 {
		t.Fatalf("revision B slots outcome = %q, setup reads = %d; want insurance_not_cleared before inventory", outcome, rig.records.SchedulerSetupCalls)
	}
	if !strings.Contains(logs, "insurance_catalog_installed revision=rev-b previous_revision=rev-a") {
		t.Fatalf("logs = %s", logs)
	}
}

func TestInsuranceRefreshKeepsServingTheCurrentCatalogWhenProductFails(t *testing.T) {
	invalid := withoutPlan(t, insurancetest.Catalog(), removedPlan)
	invalid.Regions[0].Plans[0].Family = ""
	tests := []struct {
		name     string
		breakIt  func(rig *refreshRig) (context.Context, func())
		category string
		cause    string
	}{
		{"error status", func(rig *refreshRig) (context.Context, func()) {
			rig.product.fail(http.StatusServiceUnavailable)
			return context.Background(), func() {}
		}, "upstream_status", "HTTP 503"},
		{"timeout", func(rig *refreshRig) (context.Context, func()) {
			release := make(chan struct{})
			rig.product.hangUntil(release)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			return ctx, func() { cancel(); close(release) }
		}, "timeout", "timeout"},
		{"unreachable", func(rig *refreshRig) (context.Context, func()) {
			rig.product.server.Close()
			return context.Background(), func() {}
		}, "network", "network"},
		{"invalid catalog", func(rig *refreshRig) (context.Context, func()) {
			rig.product.publishBytes("rev-b", marshal(t, invalid))
			return context.Background(), func() {}
		}, "invalid_response", "first invalid_family"},
		{"not a catalog", func(rig *refreshRig) (context.Context, func()) {
			rig.product.publishBytes("rev-b", []byte(`{"plans":[]}`))
			return context.Background(), func() {}
		}, "invalid_response", `unknown field \"plans\"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newRefreshRig(t)
			rig.install(t, "rev-a", insurancetest.Catalog())
			want := rig.plans(t, "hollywood")
			ctx, release := tc.breakIt(rig)
			defer release()

			var w *httptest.ResponseRecorder
			logs := captureLogs(func() { w = rig.refresh(t, ctx, refreshSchedulerToken) })

			if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"status":"unavailable"}` {
				t.Fatalf("status=%d body=%q, want the redacted 503", w.Code, w.Body)
			}
			if got := rig.plans(t, "hollywood"); !reflect.DeepEqual(got, want) {
				t.Fatalf("after a failed refresh the plan list is %s, want revision A unchanged", got.CatalogRevisionID)
			}
			if !strings.Contains(logs, "insurance_catalog_refresh_failed current_revision=rev-a category="+tc.category) || !strings.Contains(logs, tc.cause) {
				t.Fatalf("logs = %s, want the failure with %s and %q", logs, tc.category, tc.cause)
			}
			if strings.Contains(logs, productInsuranceToken) {
				t.Fatal("logs exposed the Product token")
			}
		})
	}
}

func TestInsuranceRefreshNeverServesAMixedRevision(t *testing.T) {
	rig := newRefreshRig(t)
	a := insurancetest.Catalog()
	b := withoutPlan(t, a, removedPlan)
	rig.install(t, "rev-b", b)
	want := map[string]insurance.PlanList{"rev-b": rig.plans(t, "hollywood")}
	rig.install(t, "rev-a", a)
	want["rev-a"] = rig.plans(t, "hollywood")

	swaps := 0
	seen := map[string]int{}
	previous := log.Writer()
	log.SetOutput(safelog.NewWriter(io.Discard))
	t.Cleanup(func() { log.SetOutput(previous) })
	revisions := []struct {
		id      string
		catalog []byte
	}{{"rev-b", marshal(t, b)}, {"rev-a", marshal(t, a)}}
	done := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for ; ; swaps++ {
			select {
			case <-done:
				return
			default:
			}
			next := revisions[swaps%2]
			rig.product.publishBytes(next.id, next.catalog)
			if w := rig.refresh(t, context.Background(), refreshSchedulerToken); w.Code != http.StatusNoContent {
				t.Errorf("refresh to %s: status=%d", next.id, w.Code)
				return
			}
		}
	}()

	var mu sync.Mutex
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 150 {
				got := rig.plans(t, "hollywood")
				if expected, ok := want[got.CatalogRevisionID]; !ok || !reflect.DeepEqual(got, expected) {
					t.Errorf("plan list for %q is not a complete revision", got.CatalogRevisionID)
					return
				}
				mu.Lock()
				seen[got.CatalogRevisionID]++
				mu.Unlock()
			}
		}()
	}
	readers.Wait()
	close(done)
	writer.Wait()
	if swaps < 2 || seen["rev-a"] == 0 || seen["rev-b"] == 0 {
		t.Fatalf("swaps=%d seen=%v; the readers did not overlap both revisions", swaps, seen)
	}
}
