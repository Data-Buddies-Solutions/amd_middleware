package session

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"advancedmd-token-management/internal/safeerrors"
)

func TestMaintainReportsLoginFailureCategories(t *testing.T) {
	const webserver = `<PPMDResults><Results success="0"><usercontext webserver="https://webserver.example.test/practicemanager"></usercontext></Results></PPMDResults>`
	tests := []struct {
		name   string
		step1  string
		step2  string
		wanted safeerrors.Category
	}{
		{
			name:   "no webserver granted",
			step1:  `<PPMDResults><Results success="0"><usercontext></usercontext></Results><Error><Fault><detail><description>Office key is not enabled for this application</description></detail></Fault></Error></PPMDResults>`,
			wanted: safeerrors.CategoryAuthentication,
		},
		{
			name:   "token refused",
			step1:  webserver,
			step2:  `<PPMDResults><Results success="0"><usercontext></usercontext></Results></PPMDResults>`,
			wanted: safeerrors.CategoryAuthentication,
		},
		{
			name:   "empty token",
			step1:  webserver,
			step2:  `<PPMDResults><Results success="1"><usercontext>  </usercontext></Results></PPMDResults>`,
			wanted: safeerrors.CategoryAuthentication,
		},
		{
			name:   "malformed login response",
			step1:  `not xml`,
			wanted: safeerrors.CategoryInvalidResponse,
		},
		{
			name:   "unbuildable token request",
			step1:  `<PPMDResults><Results success="0"><usercontext webserver="https://webserver example"></usercontext></Results></PPMDResults>`,
			wanted: safeerrors.CategoryInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := tt.step2
				if request.URL.Host == "partnerlogin.advancedmd.com" {
					body = tt.step1
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}
			err := NewSession(Credentials{Username: "user", Password: "password", OfficeKey: "office", AppName: "app"}, client).
				Maintain(context.Background())
			if got := safeerrors.Classify(err); got != tt.wanted {
				t.Fatalf("Classify(%v) = %q, want %q", err, got, tt.wanted)
			}
		})
	}
}

func TestSessionUnavailableErrorIsUnavailable(t *testing.T) {
	if got := safeerrors.Classify(ErrSessionUnavailable); got != safeerrors.CategoryUnavailable {
		t.Fatalf("Classify(ErrSessionUnavailable) = %q, want unavailable", got)
	}
}
