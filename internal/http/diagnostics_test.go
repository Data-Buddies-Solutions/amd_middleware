package http

import (
	"bytes"
	"encoding/json"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"

	"github.com/go-chi/chi/v5"
)

func TestProviderFailureCorrelatesResponseAndLogDespiteHTTP200(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer upstream.Close()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	router := chi.NewRouter()
	router.Use(requestIDMiddleware)
	router.Use(loggingMiddleware(nil))
	router.Post("/api/appointment/book", func(w http.ResponseWriter, r *http.Request) {
		_, err := clients.NewAdvancedMDRestClient(upstream.Client()).BookAppointment(r.Context(), &session.TokenData{RestApiBase: strings.TrimPrefix(upstream.URL, "https://")}, clients.BookAppointmentParams{})
		if err == nil {
			t.Error("expected upstream failure")
		}
		recordRequestOutcome(r.Context(), outcomeProviderFailure, safeerrors.CategoryUpstreamStatus)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "error", "outcome": "indeterminate_write"})
	})
	req := httptest.NewRequest(http.MethodPost, "/api/appointment/book", nil)
	const requestID = "fb672b37-0211-4e69-baf1-b0f56b181911"
	req.Header.Set("X-Request-ID", requestID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("X-Request-ID") != requestID {
		t.Fatalf("response correlation/status lost: %v", w)
	}
	if w.Header().Get("X-Abita-Outcome") != "provider_failure" {
		t.Fatalf("outcome header=%v", w.Header())
	}
	var failures []safeerrors.ProviderDiagnostic
	if err := json.Unmarshal([]byte(w.Header().Get("X-Abita-Provider-Errors")), &failures); err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].HTTPStatus != 503 {
		t.Fatalf("failures=%+v", failures)
	}
	entry := decodeLastLogEntry(t, logs.String())
	if entry["request_id"] != requestID || entry["http_status"] != float64(200) || entry["provider_error_count"] != float64(1) {
		t.Fatalf("entry=%v", entry)
	}
	assertProviderCalls(t, entry, []string{"book_appointment"}, []float64{1})
	if !strings.Contains(w.Body.String(), "indeterminate_write") {
		t.Fatal("diagnostics changed the domain outcome")
	}
}

func TestRequestLogSummarizesEveryProviderCallByOperation(t *testing.T) {
	for _, tc := range []struct {
		name           string
		body           string
		wantOperations []string
		wantCalls      []float64
	}{
		{
			name:           "successful resolve",
			body:           `{"phone":"9542872010","office":"Spring Hill"}`,
			wantOperations: []string{"get_appointments_by_month", "getdemographic", "lookuppatient"},
			wantCalls:      []float64{6, 1, 1},
		},
		{
			name: "invalid request makes no provider call",
			body: `{}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previous) })
			router := NewRouter(newPatientResolveTestHandlers(t, http.StatusOK), "test-secret", nil)
			req := httptest.NewRequest(http.MethodPost, "/api/patient/resolve", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer test-secret")
			router.ServeHTTP(httptest.NewRecorder(), req)

			entry := decodeLastLogEntry(t, logs.String())
			if tc.wantOperations == nil {
				if entry["outcome_category"] != "invalid_request" {
					t.Fatalf("outcome_category = %v, want invalid_request", entry["outcome_category"])
				}
				if calls, present := entry["provider_calls"]; present {
					t.Fatalf("provider_calls = %#v, want no key", calls)
				}
				return
			}
			assertProviderCalls(t, entry, tc.wantOperations, tc.wantCalls)
		})
	}
}

func assertProviderCalls(t *testing.T, entry map[string]any, wantOperations []string, wantCalls []float64) {
	t.Helper()
	summaries, ok := entry["provider_calls"].([]any)
	if !ok || len(summaries) != len(wantOperations) {
		t.Fatalf("provider_calls = %#v, want %v", entry["provider_calls"], wantOperations)
	}
	for i, raw := range summaries {
		summary, _ := raw.(map[string]any)
		if keys := slices.Sorted(maps.Keys(summary)); !slices.Equal(keys, []string{"calls", "maxMs", "operation", "totalMs"}) {
			t.Errorf("provider_calls[%d] keys = %v, want exactly calls, maxMs, operation, totalMs", i, keys)
		}
		if summary["operation"] != wantOperations[i] || summary["calls"] != wantCalls[i] {
			t.Errorf("provider_calls[%d] = %#v, want %s called %v times", i, raw, wantOperations[i], wantCalls[i])
		}
		total, totalOK := summary["totalMs"].(float64)
		longest, maxOK := summary["maxMs"].(float64)
		if !totalOK || !maxOK || longest < 0 || longest > total {
			t.Errorf("provider_calls[%d] durations = %#v, want 0 <= maxMs <= totalMs", i, raw)
		}
	}
}
