package http

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
)

func TestInsuranceDecisionHTTPContract(t *testing.T) {
	handlers := NewHandlers(nil, nil, nil, testNow)
	office, _ := domain.ResolveOffice("Hollywood")
	for _, plan := range []string{"Self Pay", "Aetna", "unknown words"} {
		w := httptest.NewRecorder()
		body := `{"plan":"` + plan + `","coverageType":"medical","office":"Hollywood","dob":"01/15/1980"}`
		handlers.HandleInsuranceDecision(w, httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(body)))
		var got insurance.InsuranceDecision
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		want := insurance.DecideInsurance(plan, "medical", office, "01/15/1980", testNow())
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: response=%s want=%+v", plan, w.Body.String(), want)
		}
		if strings.Contains(w.Body.String(), "routing") || strings.Contains(w.Body.String(), "credentialedProviders") {
			t.Fatalf("dropped field returned: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	body := `{"plan":"Medicare","coverageType":"medical","office":"Spring Hill","offeredPlanIds":["aetna-epo","aetna-hmo","aetna-medicare"]}`
	handlers.HandleInsuranceDecision(w, httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(body)))
	if !strings.Contains(w.Body.String(), `"planId":"aetna-medicare"`) {
		t.Fatalf("offered answer response=%s", w.Body.String())
	}
}

func TestInsuranceRoutesRejectInvalidInputWithTheSharedErrorContract(t *testing.T) {
	for _, tc := range []struct{ name, method, target, body string }{
		{"decision caller verification flag", http.MethodPost, "/api/insurance/decision", `{"plan":"Self Pay","coverageType":"medical","office":"Hollywood","referralVerified":true}`},
		{"decision invalid JSON", http.MethodPost, "/api/insurance/decision", `{"plan":`},
		{"decision unknown office", http.MethodPost, "/api/insurance/decision", `{"plan":"Aetna","coverageType":"medical","office":"atlantis"}`},
		{"plans unknown office", http.MethodGet, "/api/insurance/plans?office=atlantis&coverage=medical", ""},
		{"plans missing office", http.MethodGet, "/api/insurance/plans?coverage=medical", ""},
		{"plans empty office", http.MethodGet, "/api/insurance/plans?office=&coverage=medical", ""},
		{"plans unknown coverage", http.MethodGet, "/api/insurance/plans?office=hollywood&coverage=dental", ""},
		{"plans missing coverage", http.MethodGet, "/api/insurance/plans?office=hollywood", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previousWriter := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousWriter) })
			router := NewRouter(NewHandlers(nil, nil, nil, testNow), "test-secret", nil)
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer test-secret")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			var got errorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("body is not JSON: %q", w.Body.String())
			}
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || got.Status != "error" || got.Message == "" {
				t.Fatalf("status=%d type=%q body=%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
			}
			entry := decodeLastLogEntry(t, logs.String())
			if entry["outcome_category"] != "invalid_request" || entry["provider_failure_category"] != "none" {
				t.Fatalf("log=%v", entry)
			}
		})
	}
}

func getInsurancePlans(t *testing.T, query, auth string) *httptest.ResponseRecorder {
	t.Helper()
	router := NewRouter(NewHandlers(nil, nil, nil, testNow), "test-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/insurance/plans?"+query, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestInsurancePlansHTTPContract(t *testing.T) {
	for _, tc := range []struct{ key, officeID, coverage string }{
		{"hollywood", "hollywood", "medical"},
		{"spring-hill", "spring_hill", "routine_vision"},
		{"north-miami-beach-optical", "north_miami_beach_optical", "routine_vision"},
	} {
		w := getInsurancePlans(t, "office="+tc.key+"&coverage="+tc.coverage, "Bearer test-secret")
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: status=%d body=%s", tc.key, w.Code, w.Body.String())
		}
		var got struct {
			OfficeID string                  `json:"officeId"`
			Coverage string                  `json:"coverage"`
			Plans    []insurance.PlanSummary `json:"plans"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		office, _ := domain.LookupOfficeByID(tc.officeID)
		want, _ := insurance.ListPlans(tc.coverage, office)
		if got.OfficeID != tc.officeID || got.Coverage != tc.coverage || len(want) == 0 || !reflect.DeepEqual(got.Plans, want) {
			t.Fatalf("%s: response=%s", tc.key, w.Body.String())
		}
	}
}

func TestInsurancePlansOfficeWithoutTheCoverageReturnsNoPlans(t *testing.T) {
	w := getInsurancePlans(t, "office=north-miami-beach-optical&coverage=medical", "Bearer test-secret")
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"officeId":"north_miami_beach_optical","coverage":"medical","plans":[]}` {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestInsurancePlansRequiresTheAPISecret(t *testing.T) {
	for _, auth := range []string{"", "Bearer wrong"} {
		w := getInsurancePlans(t, "office=hollywood&coverage=medical", auth)
		if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), "planId") {
			t.Fatalf("auth %q: status=%d body=%s", auth, w.Code, w.Body.String())
		}
	}
}
