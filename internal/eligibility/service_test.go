package eligibility

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func input() CheckInput {
	return CheckInput{FirstName: "Jane", LastName: "Sample", DOB: "1980-01-02", MemberID: "synthetic", Plan: "oscar"}
}
func fixture(benefits, extra string) string {
	return `{"meta":{"applicationMode":"production"},"eligibilitySearchId":"search-one","id":"check-one","subscriber":{"firstName":"Jane","lastName":"Sample","dateOfBirth":"19800102","memberId":"synthetic"},"benefitsInformation":` + benefits + extra + `}`
}
func testService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, err := New("secret", map[string]Provider{"office": {OrganizationName: "Synthetic Practice", NPI: "1999999984"}})
	if err != nil {
		t.Fatal(err)
	}
	service.url = server.URL
	service.now = func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }
	return service
}
func TestIntakeOutcomesAndFiveInputWireContract(t *testing.T) {
	for _, tc := range []struct{ name, body, status, reason string }{
		{"active", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""), "active", ""},
		{"inactive", fixture(`[{"code":"6","serviceTypeCodes":["30"]}]`, ""), "inactive", ""},
		{"other service", fixture(`[{"code":"1","serviceTypeCodes":["98"]}]`, ""), "review", "general_coverage_unknown"},
		{"conflict", fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"6","serviceTypeCodes":["30"]}]`, ""), "review", "general_coverage_unknown"},
		{"payer rejection overrides active", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, `,"errors":[{"code":"75"}]`), "review", "payer_rejected"},
		{"dependent returned", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, `,"dependents":[{"firstName":"Jane","lastName":"Sample","dateOfBirth":"19800102"}]`), "review", "subscriber_details_required"},
		{"different subscriber", strings.ReplaceAll(fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""), `"Jane"`, `"Parent"`), "review", "identity_uncertain"},
		{"empty benefit", fixture(`[{}]`, ""), "unknown", "unrecognized_response"},
		{"null benefit", fixture(`[null]`, ""), "unknown", "unrecognized_response"},
		{"empty rejection", fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, `,"errors":[{}]`), "unknown", "unrecognized_response"},
		{"null rejection", fixture(`[]`, `,"errors":[null]`), "unknown", "unrecognized_response"},
		{"empty", "{}", "unknown", "unrecognized_response"},
		{"invalid", "not json", "unknown", "unrecognized_response"},
		{"test mode", `{"meta":{"applicationMode":"test"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`, "unknown", "nonproduction_or_unknown_mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			service := testService(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				for _, forbidden := range []string{"dateOfService", "dependents", "eligibilitySearchId", "patientId", "appointmentId", "history"} {
					if strings.Contains(string(raw), `"`+forbidden+`"`) {
						t.Errorf("unwanted wire field %s", forbidden)
					}
				}
				var request Request
				if json.Unmarshal(raw, &request) != nil {
					t.Fatal("bad request")
				}
				if request.Payer != "OSCAR" || request.Provider.OrganizationName != "Synthetic Practice" || request.Provider.NPI != "1999999984" || r.Header.Get("Authorization") != "secret" {
					t.Fatal("wrong route/provider/auth")
				}
				if len(request.Encounter.ServiceTypeCodes) != 1 || request.Encounter.ServiceTypeCodes[0] != "30" {
					t.Fatal("must only send STC30")
				}
				if request.Subscriber.FirstName != "Jane" || request.Subscriber.LastName != "Sample" || request.Subscriber.DateOfBirth != "19800102" || request.Subscriber.MemberID != "synthetic" {
					t.Fatal("patient data altered")
				}
				fmt.Fprint(w, tc.body)
			})
			result, err := service.Check(context.Background(), "office", input())
			if err != nil || calls != 1 || result.Status != tc.status || result.ReviewReason != tc.reason {
				t.Fatalf("%+v err=%v calls=%d", result, err, calls)
			}
			if result.CheckedAt.IsZero() || result.OfficeID != "office" {
				t.Fatal("missing check context")
			}
		})
	}
}
func TestRoutingAndValidationDoNotSend(t *testing.T) {
	service := testService(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	for _, plan := range []string{"icare", "medicaid", "medicare", "unknown", "car40907", "self pay"} {
		in := input()
		in.Plan = plan
		result, err := service.Check(context.Background(), "office", in)
		if err != nil || result.Status != "review" || result.ReviewReason == "" {
			t.Fatalf("%s: %+v %v", plan, result, err)
		}
	}
	for _, change := range []func(*CheckInput){func(in *CheckInput) { in.FirstName = "" }, func(in *CheckInput) { in.LastName = "" }, func(in *CheckInput) { in.DOB = "2026-02-30" }, func(in *CheckInput) { in.MemberID = "" }, func(in *CheckInput) { in.Plan = "" }} {
		in := input()
		change(&in)
		if _, err := service.Check(context.Background(), "office", in); err == nil {
			t.Fatal("missing/invalid clinical input accepted")
		}
	}
	if result, err := service.Check(context.Background(), "unconfigured", input()); err != nil || result.ReviewReason != "provider_not_configured" {
		t.Fatal("unconfigured provider must not be guessed")
	}
}
func TestCurrentOfficeDateSelectsRouteInternally(t *testing.T) {
	var payer string
	service := testService(t, func(w http.ResponseWriter, r *http.Request) {
		var request Request
		_ = json.NewDecoder(r.Body).Decode(&request)
		payer = request.Payer
		fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""))
	})
	in := input()
	in.Plan = "Children's Medical Services"
	for _, tc := range []struct {
		hour int
		want string
	}{{3, "68069"}, {4, "51062"}} {
		service.now = func() time.Time { return time.Date(2026, 10, 1, tc.hour, 0, 0, 0, time.UTC) }
		if _, err := service.Check(context.Background(), "office", in); err != nil || payer != tc.want {
			t.Fatalf("UTC hour %d payer=%s err=%v", tc.hour, payer, err)
		}
	}
}
func TestHTTPFailuresNeverRetry(t *testing.T) {
	for _, code := range []int{302, 429, 500} {
		calls := 0
		service := testService(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Location", "/redirect")
			w.WriteHeader(code)
		})
		result, err := service.Check(context.Background(), "office", input())
		if err != nil || calls != 1 || result.Status != "unknown" {
			t.Fatalf("%+v err=%v calls=%d", result, err, calls)
		}
	}
}

// Financial fields and future payer fields must survive independently of assessment.
func TestCompleteProviderEvidenceSurvivesAssessment(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		httpStatus  int
	}{
		{"active", "", 200},
		{"review", `,"errors":[{"code":"75","description":"Synthetic rejection","futureError":{"value":true}}]`, 200},
		{"http failure", "", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fixture(`[{"code":"1","serviceTypeCodes":["30"]},{"code":"B","serviceTypeCodes":["98"],"benefitAmount":"35","inPlanNetworkIndicatorCode":"Y"},{"code":"C","serviceTypeCodes":["30"],"benefitAmount":"1500","timeQualifierCode":"29"},{"code":"A","benefitPercent":"0.2","coverageLevelCode":"FAM"}]`, `,"planInformation":{"groupNumber":"synthetic"},"futureField":{"sequence":9007199254740993,"values":[null,true,"text"]}`+tc.extra)
			s := testService(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.httpStatus); fmt.Fprint(w, body) })
			result, err := s.Check(context.Background(), "office", input())
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(result)
			var output map[string]json.RawMessage
			json.Unmarshal(encoded, &output)
			var want, got bytes.Buffer
			if json.Compact(&want, []byte(body)) != nil || json.Compact(&got, output["providerResponse"]) != nil || !bytes.Equal(want.Bytes(), got.Bytes()) {
				t.Fatal("full provider response was not preserved")
			}
			if string(output["providerHttpStatus"]) != fmt.Sprint(tc.httpStatus) {
				t.Fatal("HTTP status missing")
			}
			if tc.httpStatus != 200 && result.Status != "unknown" {
				t.Fatal("failed response established coverage")
			}
		})
	}
}

func TestInvalidProviderJSONRemainsEvidenceNotCoverage(t *testing.T) {
	body := "synthetic upstream error"
	s := testService(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
	result, err := s.Check(context.Background(), "office", input())
	if err != nil || result.Status != "unknown" || result.ReviewReason != "unrecognized_response" {
		t.Fatalf("unexpected assessment: %+v %v", result, err)
	}
	var retained string
	if err := json.Unmarshal(result.ProviderResponse, &retained); err != nil || retained != body {
		t.Fatal("invalid JSON evidence discarded")
	}
}

func TestCorrectedPatientOnlyExposedAfterTrustedResponse(t *testing.T) {
	for _, tc := range []struct {
		name, extra, mode string
		want              bool
	}{
		{"supported correction", "", "production", true},
		{"payer rejection", `,"errors":[{"code":"75"}]`, "production", false},
		{"dependent", `,"dependents":[{"firstName":"Jane","lastName":"Sample","dateOfBirth":"19800102","memberId":"synthetic"}]`, "production", false},
		{"test response", "", "test", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, tc.extra), `"production"`, `"`+tc.mode+`"`, 1)
			s := testService(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			in := input()
			in.FirstName = "Ane"
			in.LastName = "Sample Jr."
			got, err := s.Check(context.Background(), "office", in)
			if err != nil || (got.MatchedPatient != nil) != tc.want {
				t.Fatalf("unexpected match exposure: %+v %v", got, err)
			}
			if tc.want && (got.Status != "active" || got.Match.Status != "matched_with_name_correction" || got.MatchedPatient.FirstName != "Jane" || got.MatchedPatient.LastName != "Sample") {
				t.Fatalf("correction missing: %+v", got)
			}
		})
	}
}

func TestCrystalRiverUsesOnlyLichtWithoutProviderConfiguration(t *testing.T) {
	calls := 0
	service := testService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Provider.NPI != "1497147680" || request.Provider.FirstName != "Joseph" || request.Provider.LastName != "Licht" || request.Provider.OrganizationName != "" {
			t.Fatalf("wrong Crystal River provider: %+v", request.Provider)
		}
		fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""))
	})
	service.providers = nil
	result, err := service.Check(context.Background(), "crystal_river", input())
	if err != nil || calls != 1 || result.Status != "active" || len(result.ProviderResults) != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}
