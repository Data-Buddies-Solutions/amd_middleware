package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"advancedmd-token-management/internal/eligibility"
)

func TestEligibilityRouteAuthAndStrictInput(t *testing.T) {
	h := NewHandlers(nil, nil, nil)
	router := NewRouter(h, "secret", nil)
	req := httptest.NewRequest("POST", "/api/eligibility/check", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("eligibility route must require authentication")
	}
	req = httptest.NewRequest("POST", "/api/eligibility/check", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal("unconfigured integration must fail visibly")
	}
	s, err := eligibility.New("key", map[string]eligibility.Provider{"spring_hill": {OrganizationName: "Synthetic Practice", NPI: "1999999984"}})
	if err != nil {
		t.Fatal(err)
	}
	h.SetEligibility(s)
	for _, body := range []string{`{"serviceTypeCodes":["98"]}`, `{} {}`, `{"scope":{}}`} {
		req = httptest.NewRequest("POST", "/api/eligibility/check", strings.NewReader(body))
		req.Header.Set("Authorization", "secret")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("unexpected response %d", w.Code)
		}
	}
}

func TestEligibilityReceiptsLogSafeProviderFailures(t *testing.T) {
	const active = `{"meta":{"applicationMode":"production"},"subscriber":{"firstName":"SyntheticJane","lastName":"PrivateSample","dateOfBirth":"19800102"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`
	for _, tc := range []struct {
		name, body, status, category string
		httpStatus                   int
		err                          error
	}{
		{name: "active", body: active, status: "active", category: "none", httpStatus: 200},
		{name: "payer rejection", body: `{"meta":{"applicationMode":"production"},"errors":[{"code":"75"}]}`, status: "review", category: "rejected", httpStatus: 200},
		{name: "upstream status", status: "unknown", category: "upstream_status", httpStatus: 500},
		{name: "timeout", status: "unknown", category: "upstream_error", err: context.DeadlineExceeded},
		{name: "malformed response", body: `invalid`, status: "unknown", category: "invalid_response", httpStatus: 200},
		{name: "test mode", body: `{"meta":{"applicationMode":"test"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`, status: "unknown", category: "invalid_response", httpStatus: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previousWriter, previousTransport := log.Writer(), http.DefaultTransport
			log.SetOutput(&logs)
			t.Cleanup(func() {
				log.SetOutput(previousWriter)
				http.DefaultTransport = previousTransport
			})
			// Service uses the default transport. Intercept every outgoing request
			// so this exercises the real handler without contacting Stedi.
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.httpStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			s, err := eligibility.New("synthetic-secret", map[string]eligibility.Provider{"spring_hill": {OrganizationName: "Synthetic Practice", NPI: "1999999984"}})
			if err != nil {
				t.Fatal(err)
			}
			h := NewHandlers(nil, nil, nil)
			h.SetEligibility(s)
			router := NewRouter(h, "secret", nil)
			req := httptest.NewRequest(http.MethodPost, "/api/eligibility/check", strings.NewReader(`{"firstName":"SyntheticJane","lastName":"PrivateSample","dob":"1980-01-02","memberId":"private-member","plan":"Oscar Health","coverageType":"routine_vision"}`))
			req.Header.Set("Authorization", "secret")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			var result eligibility.Result
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Status != tc.status {
				t.Fatalf("unexpected receipt: HTTP %d, status %q", w.Code, result.Status)
			}
			entry := decodeLastLogEntry(t, logs.String())
			wantOutcome := "provider_failure"
			if tc.category == "none" {
				wantOutcome = "success"
			}
			if entry["outcome_category"] != wantOutcome || entry["provider_failure_category"] != tc.category {
				t.Fatalf("unexpected log categories: %v", entry)
			}
			for _, private := range []string{"SyntheticJane", "PrivateSample", "19800102", "private-member", "synthetic-secret"} {
				if strings.Contains(logs.String(), private) {
					t.Fatal("private evidence present in request logs")
				}
			}
		})
	}
}

func TestEligibilityUsesExistingOfficeContextAndRejectsBookingInputs(t *testing.T) {
	previousTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	calls := 0
	var providerName string
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var request eligibility.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		providerName = request.Provider.OrganizationName
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"meta":{"applicationMode":"production"},"subscriber":{"firstName":"Jane","lastName":"Sample","dateOfBirth":"19800102"},"benefitsInformation":[{"code":"1","serviceTypeCodes":["30"]}]}`)), Request: r}, nil
	})
	service, err := eligibility.New("secret", map[string]eligibility.Provider{
		"spring_hill": {OrganizationName: "Default Practice", NPI: "1999999984"},
		"hollywood":   {OrganizationName: "Hollywood Practice", NPI: "1659862753"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(nil, nil, nil)
	h.SetEligibility(service)
	router := NewRouter(h, "secret", nil)
	send := func(body map[string]any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/eligibility/check", bytes.NewReader(data))
		r.Header.Set("Authorization", "secret")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	body := map[string]any{"firstName": "Jane", "lastName": "Sample", "dob": "1980-01-02", "memberId": "synthetic", "plan": "Oscar Health", "coverageType": "routine_vision"}
	if w := send(body); w.Code != 200 || providerName != "Default Practice" {
		t.Fatal("five clinical inputs must suffice with configured default office")
	}
	body["office"] = "Hollywood"
	if w := send(body); w.Code != 200 || providerName != "Hollywood Practice" {
		t.Fatal("trusted office context must choose its configured provider")
	}
	body["office"] = "not-an-office"
	if w := send(body); w.Code != 400 || calls != 2 {
		t.Fatal("unknown office must not fall back")
	}
	delete(body, "office")
	for _, field := range []string{"history", "scope", "patientId", "appointmentId", "intakeId", "serviceDate", "provider", "subscriber", "dependent", "recordedNames"} {
		body[field] = "unused"
		if w := send(body); w.Code != 400 || calls != 2 {
			t.Errorf("old/unsupported field %s accepted", field)
		}
		delete(body, field)
	}
}
