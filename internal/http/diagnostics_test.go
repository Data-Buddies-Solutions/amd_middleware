package http

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
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
	router.Use(RequestIDMiddleware)
	router.Use(LoggingMiddleware(nil))
	router.Post("/api/appointment/book", func(w http.ResponseWriter, r *http.Request) {
		_, err := clients.NewAdvancedMDRestClient(upstream.Client()).BookAppointment(r.Context(), &domain.TokenData{RestApiBase: strings.TrimPrefix(upstream.URL, "https://")}, clients.BookAppointmentParams{})
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
	if !strings.Contains(w.Body.String(), "indeterminate_write") {
		t.Fatal("diagnostics changed the domain outcome")
	}
}
