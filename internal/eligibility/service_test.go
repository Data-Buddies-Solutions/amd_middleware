package eligibility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func input() CheckInput {
	return CheckInput{Scope: Scope{"office", "patient", "appointment", "20260916"}, Plan: "oscar", Subscriber: Person{FirstName: "Jane", LastName: "Sample", DateOfBirth: "19800102", MemberID: "synthetic"}}
}
func fixture(benefits, extra string) string {
	return `{"meta":{"applicationMode":"production"},"eligibilitySearchId":"search-one","id":"check-one","subscriber":{"firstName":"Jane","lastName":"Sample","dateOfBirth":"19800102","memberId":"synthetic"},"benefitsInformation":` + benefits + extra + `}`
}
func testService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s, err := New("secret", map[string]Provider{"office": {OrganizationName: "Synthetic Practice", NPI: "1999999984"}})
	if err != nil {
		t.Fatal(err)
	}
	s.url = server.URL
	return s
}
func TestCheckOutcomesAndWireContract(t *testing.T) {
	for _, tc := range []struct{ name, body, status, coverage, reason string }{
		{"active", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""), "completed", "active", ""},
		{"inactive", fixture(`[{"code":"6","serviceTypeCodes":["30"]}]`, ""), "completed", "inactive", ""},
		{"other service", fixture(`[{"code":"1","serviceTypeCodes":["98"]}]`, ""), "review", "unknown", "general_coverage_unknown"},
		{"conflict", fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"6","serviceTypeCodes":["30"]}]`, ""), "review", "unknown", "general_coverage_unknown"},
		{"rejected active", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, `,"errors":[{"code":"75"}]`), "payer_rejected", "unknown", "payer_rejected"},
		{"identity", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, `,"dependents":[{"firstName":"Janet","lastName":"Sample","dateOfBirth":"19800102"}]`), "review", "active", "identity_uncertain"},
		{"empty", "{}", "unknown", "unknown", "unrecognized_response"},
		{"invalid", "not json", "unknown", "unknown", "unrecognized_response"},
		{"test mode", `{"meta":{"applicationMode":"test"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`, "unknown", "unknown", "nonproduction_or_unknown_mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := testService(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "secret" {
					t.Error("missing authorization")
				}
				var request Request
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Fatal("bad request")
				}
				if request.Payer != "OSCAR" || (len(request.Encounter.ServiceTypeCodes) != 1 || request.Encounter.ServiceTypeCodes[0] != "30" || request.Encounter.DateOfService != "20260916") || request.SearchID != "" {
					t.Errorf("wrong request: %+v", request)
				}
				p := request.Provider
				if p.OrganizationName != "Synthetic Practice" {
					t.Error("provider not configured")
				}
				fmt.Fprint(w, tc.body)
			})
			out, err := s.Check(context.Background(), input())
			if err != nil || calls != 1 || out.Status != tc.status || out.Coverage != tc.coverage || out.ReviewReason != tc.reason {
				t.Fatalf("result=%+v err=%v calls=%d", out, err, calls)
			}
			if out.Scope != input().Scope || out.InputID == "" || out.Attempt != 1 || out.CheckedAt.IsZero() {
				t.Fatal("missing scope receipt")
			}
		})
	}
}
func TestRetryHistoryBoundedAndPatientScoped(t *testing.T) {
	calls := 0
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if calls == 0 && req.SearchID != "" {
			t.Error("new patient inherits chain")
		}
		if calls > 0 && req.SearchID != "search-one" {
			t.Error("missing search chain")
		}
		calls++
		fmt.Fprint(w, fixture(`[]`, `,"errors":[{"code":"75"}]`))
	})
	in := input()
	in.RecordedNames = []RecordedName{{"Janet", "Sample", "chart"}, {"Jane", "Sample Smith", "intake"}, {"Janet", "Sample Smith", "chart"}}
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		out, err := s.Check(context.Background(), in)
		if err != nil || out.Status != "payer_rejected" {
			t.Fatalf("%+v %v", out, err)
		}
		key := Fingerprint(out.Request)
		if seen[key] {
			t.Fatal("duplicate sent")
		}
		seen[key] = true
		in.History = append(in.History, out)
	}
	out, _ := s.Check(context.Background(), in)
	if calls != 4 || out.ReviewReason != "attempt_limit" {
		t.Fatalf("not bounded: %+v", out)
	}
	in.Scope.PatientID = "other-patient"
	if _, err := s.Check(context.Background(), in); err == nil || calls != 4 {
		t.Fatal("cross-patient history accepted")
	}
}
func TestTerminalAndUnknownHistoryNeverResent(t *testing.T) {
	for _, body := range []string{`{}`, fixture(`[]`, `,"errors":[{"code":"71"}]`), fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, "")} {
		calls := 0
		s := testService(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, body) })
		in := input()
		out, _ := s.Check(context.Background(), in)
		in.History = []Result{out}
		out, err := s.Check(context.Background(), in)
		if err != nil || calls != 1 || out.ReviewReason != "history_terminal_or_unknown" {
			t.Fatalf("resent: %+v %v", out, err)
		}
	}
}
func TestRoutingAndValidationDoNotSend(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	for _, plan := range []string{"icare", "medicaid", "medicare", "original medicare", "unknown", "car40907", "self pay"} {
		in := input()
		in.Plan = plan
		out, err := s.Check(context.Background(), in)
		if err != nil || out.Status != "review" || out.ReviewReason == "" {
			t.Fatalf("%s: %+v %v", plan, out, err)
		}
	}
	in := input()
	in.Subscriber.MemberID = ""
	if _, err := s.Check(context.Background(), in); err == nil {
		t.Fatal("member required")
	}
	in = input()
	in.Scope.ServiceDate = "20260230"
	if _, err := s.Check(context.Background(), in); err == nil {
		t.Fatal("valid service date required")
	}
	for _, plan := range []string{"aetna commercial ppo", "aetna medicare hmo"} {
		if payer, reason := Route(plan, "20260916"); payer != "60054" || reason != "" {
			t.Fatal("explicit product route failed")
		}
	}
}
func TestHTTPFailureAndRedirectAreUnknownWithoutRetry(t *testing.T) {
	for _, code := range []int{302, 429, 500} {
		calls := 0
		s := testService(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Location", "/redirect")
			w.WriteHeader(code)
		})
		out, err := s.Check(context.Background(), input())
		if err != nil || calls != 1 || out.Status != "unknown" {
			t.Fatalf("%+v %v calls=%d", out, err, calls)
		}
	}
}

func TestRetryRejectsChangedProvider(t *testing.T) {
	calls := 0
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, fixture(`[]`, `,"errors":[{"code":"75"}]`))
	})
	in := input()
	out, err := s.Check(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.History = []Result{out}
	s.providers["office"] = Provider{OrganizationName: "Changed Practice", NPI: "1999999984"}
	if _, err = s.Check(context.Background(), in); err == nil || calls != 1 {
		t.Fatal("provider change reused chain")
	}
}
func TestReplayUsesSameCoverageRules(t *testing.T) {
	for _, body := range []string{
		fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""),
		fixture(`[{"code":"1","serviceTypeCodes":["98"]}]`, ""),
		fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"6","serviceTypeCodes":["30"]}]`, ""),
		`{"meta":{"applicationMode":"test"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`,
	} {
		s := testService(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
		out, err := s.Check(context.Background(), input())
		if err != nil {
			t.Fatal(err)
		}
		replay, err := Assess(Case{Expected: input().Subscriber, Response: json.RawMessage(body), Source: "eligibility"})
		if err != nil {
			t.Fatal(err)
		}
		if replay.Coverage != out.Coverage || replay.ActiveResponse != (out.Coverage == "active") {
			t.Fatal("replay disagrees with live interpretation")
		}
	}
}
