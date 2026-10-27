package advancedmd

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"
)

func TestAdapterLogsInAgainAfterProviderRejectsToken(t *testing.T) {
	const callers = 8
	const rejectedRequests = 2 * callers
	var logins, rejections atomic.Int32
	var arrived sync.WaitGroup
	arrived.Add(rejectedRequests)
	var sentTokens sync.Map
	client := &http.Client{Transport: providerRoundTrip(func(request *http.Request) (int, string) {
		switch {
		case request.URL.Host == "partnerlogin.advancedmd.com":
			return http.StatusOK, `<PPMDResults><Results success="0"><usercontext webserver="https://webserver.advancedmd.test/processrequest/api-801/app"></usercontext></Results></PPMDResults>`
		case strings.Contains(request.URL.Path, "/xmlrpc/"):
			if logins.Add(1) == 1 {
				return http.StatusOK, `<PPMDResults><Results success="1"><usercontext>revoked</usercontext></Results></PPMDResults>`
			}
			return http.StatusOK, `<PPMDResults><Results success="1"><usercontext>replacement</usercontext></Results></PPMDResults>`
		}
		token := request.Header.Get("Authorization")
		sentTokens.Store(token, true)
		if token == "Bearer revoked" {
			if rejections.Add(1) <= rejectedRequests {
				arrived.Done()
				arrived.Wait()
			}
			return http.StatusUnauthorized, ``
		}
		return http.StatusOK, `[]`
	})}
	adapter := NewAdapter(
		session.NewSession(session.Credentials{Username: "user", Password: "password", OfficeKey: "office", AppName: "app"}, client, time.Now),
		nil,
		clients.NewAdvancedMDRestClient(client),
		time.Now,
	)

	failures := make(chan error, callers)
	for range callers {
		go func() {
			_, err := adapter.ReadColumnDay(context.Background(), "101", "2026-10-26")
			failures <- err
		}()
	}
	for range callers {
		if err := <-failures; CategoryOf(err) != safeerrors.CategoryAuthentication {
			t.Fatalf("ReadColumnDay() with rejected token error = %v, want authentication", err)
		}
	}

	if _, err := adapter.ReadColumnDay(context.Background(), "101", "2026-10-26"); err != nil {
		t.Fatalf("ReadColumnDay() after rejection error = %v", err)
	}
	if got := logins.Load(); got != 2 {
		t.Fatalf("logins = %d, want 2", got)
	}
	if _, sent := sentTokens.Load("Bearer replacement"); !sent {
		t.Fatal("request after rejection did not send the replacement token")
	}
}

type providerRoundTrip func(*http.Request) (int, string)

func (f providerRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	status, body := f(request)
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}
