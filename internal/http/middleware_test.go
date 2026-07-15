package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDMiddlewareHashesCallerValueForLogs(t *testing.T) {
	var requestID string
	var logRequestID string
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID = GetRequestID(r.Context())
		logRequestID = GetLogRequestID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "patientId=17604634 token=secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "patientId=17604634 token=secret" {
		t.Fatalf("X-Request-ID = %q, want caller value echoed", got)
	}
	if requestID != "patientId=17604634 token=secret" {
		t.Fatalf("request ID = %q, want caller value retained", requestID)
	}
	if !strings.HasPrefix(logRequestID, "external-") {
		t.Fatalf("log request ID = %q, want hashed external ID", logRequestID)
	}
	for _, forbidden := range []string{"17604634", "secret"} {
		if strings.Contains(logRequestID, forbidden) {
			t.Fatalf("log request ID %q exposed %q", logRequestID, forbidden)
		}
	}
}
