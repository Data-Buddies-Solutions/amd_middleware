package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

func TestProviderDiagnosticsPreserveStatusAndNumericFaultWithoutPayload(t *testing.T) {
	for _, test := range []struct {
		name                 string
		status               int
		body, category, code string
	}{
		{"http status", 503, "patient-secret", "upstream_status", ""},
		{"numeric fault", 200, `{"PPMDResults":{"Error":{"Fault":{"faultcode":"-32602","description":"patient-secret"}}}}`, "rejected", "-32602"},
		{"non-200 rejection", 201, `{"PPMDResults":{"Error":{"Fault":{"faultcode":"42"}}}}`, "rejected", "42"},
		{"text fault", 200, `{"PPMDResults":{"Error":{"Fault":{"faultcode":"patient-secret"}}}}`, "rejected", ""},
		{"malformed", 200, "patient-secret", "invalid_response", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer provider.Close()
			ctx, diagnostics := safeerrors.WithDiagnostics(context.Background())
			client := NewAdvancedMDClient(provider.Client())
			_, err := client.GetDemographic(ctx, &domain.TokenData{XmlrpcURL: strings.TrimPrefix(provider.URL, "https://")}, "synthetic-patient")
			if err == nil {
				t.Fatal("expected failure")
			}
			failures, count := diagnostics.Snapshot()
			if count != 1 || len(failures) != 1 {
				t.Fatalf("diagnostics = %+v", failures)
			}
			got := failures[0]
			if got.Operation != "getdemographic" || string(got.Category) != test.category || got.Code != test.code {
				t.Fatalf("diagnostic = %+v", got)
			}
			if got.HTTPStatus != test.status {
				t.Fatalf("status = %d", got.HTTPStatus)
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "patient-secret") || strings.Contains(string(encoded), "synthetic-patient") {
				t.Fatalf("unsafe diagnostic: %s", encoded)
			}
		})
	}
}

func TestRestMutationDiagnosticsDoNotChangeAmbiguousWriteDisposition(t *testing.T) {
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer provider.Close()
	ctx, diagnostics := safeerrors.WithDiagnostics(context.Background())
	_, err := NewAdvancedMDRestClient(provider.Client()).BookAppointment(ctx, &domain.TokenData{RestApiBase: strings.TrimPrefix(provider.URL, "https://")}, BookAppointmentParams{})
	if MutationDispositionOf(err) != MutationDispositionAmbiguous {
		t.Fatalf("mutation disposition changed: %v", err)
	}
	failures, count := diagnostics.Snapshot()
	if count != 1 || failures[0].HTTPStatus != 503 || failures[0].Operation != "book_appointment" {
		t.Fatalf("diagnostics = %+v", failures)
	}
}
