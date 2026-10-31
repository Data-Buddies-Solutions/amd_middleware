package clients

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/safeerrors"
)

const productTestToken = "product-insurance-token"

func TestProductClientReadsTheInsuranceRevision(t *testing.T) {
	var got struct{ method, path, authorization, contentType, body string }
	product := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.method, got.path, got.body = r.Method, r.URL.Path, string(body)
		got.authorization, got.contentType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revisionId":"0d9f6f0e-1111-4a5b-8c9d-123456789abc","catalog":{"carriers":[],"regions":[]}}`))
	}))
	defer product.Close()

	id, catalog, err := NewProductClient(product.Client(), product.URL, productTestToken).ReadInsuranceCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id != "0d9f6f0e-1111-4a5b-8c9d-123456789abc" || string(catalog) != `{"carriers":[],"regions":[]}` {
		t.Fatalf("revision = %q catalog = %s", id, catalog)
	}
	if got.method != http.MethodPost || got.path != "/v1/agent/insurance/read" || got.body != "{}" ||
		got.authorization != "Bearer "+productTestToken || got.contentType != "application/json" {
		t.Fatalf("request = %+v", got)
	}
}

func TestProductClientReportsEveryFailureWithoutTheToken(t *testing.T) {
	reply := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(body))
		}
	}
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		category safeerrors.Category
		message  string
	}{
		{"unavailable", reply(http.StatusServiceUnavailable, `{"error":{"code":"UNAVAILABLE"}}`), safeerrors.CategoryUpstreamStatus, "HTTP 503"},
		{"credential rejected", reply(http.StatusUnauthorized, `{"error":{"code":"UNAUTHENTICATED"}}`), safeerrors.CategoryUpstreamStatus, "HTTP 401"},
		{"not JSON", reply(http.StatusOK, `<html>`), safeerrors.CategoryInvalidResponse, "not an insurance revision"},
		{"unknown field", reply(http.StatusOK, `{"revisionId":"r","catalog":{},"practiceId":"p"}`), safeerrors.CategoryInvalidResponse, `unknown field "practiceId"`},
		{"no revision", reply(http.StatusOK, `{"catalog":{}}`), safeerrors.CategoryInvalidResponse, "no revisionId or catalog"},
		{"no catalog", reply(http.StatusOK, `{"revisionId":"r"}`), safeerrors.CategoryInvalidResponse, "no revisionId or catalog"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			product := httptest.NewServer(tc.handler)
			defer product.Close()
			_, _, err := NewProductClient(product.Client(), product.URL, productTestToken).ReadInsuranceCatalog(context.Background())
			assertProductFailure(t, err, tc.category, tc.message)
		})
	}
}

func TestProductClientReportsTimeoutsAndUnreachableProduct(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := NewProductClient(slow.Client(), slow.URL, productTestToken).ReadInsuranceCatalog(ctx)
	assertProductFailure(t, err, safeerrors.CategoryTimeout, "timeout")

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	_, _, err = NewProductClient(http.DefaultClient, closed.URL, productTestToken).ReadInsuranceCatalog(context.Background())
	assertProductFailure(t, err, safeerrors.CategoryNetwork, "network")
}

func assertProductFailure(t *testing.T, err error, category safeerrors.Category, message string) {
	t.Helper()
	if err == nil {
		t.Fatal("read succeeded")
	}
	if got := safeerrors.Classify(err); got != category {
		t.Fatalf("category = %s, want %s (%v)", got, category, err)
	}
	if !strings.Contains(err.Error(), message) || strings.Contains(err.Error(), productTestToken) {
		t.Fatalf("error = %q, want %q and no token", err, message)
	}
}
