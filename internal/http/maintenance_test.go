package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/session"
)

func TestMaintenanceRouteRequiresDedicatedSchedulerIdentity(t *testing.T) {
	const (
		agentSecret    = "agent-api-secret"
		schedulerToken = "scheduler-id-token"
		audience       = "https://middleware.example.test"
		schedulerEmail = "middleware-maintenance@example.iam.gserviceaccount.com"
	)

	session := &recordingMaintenanceSession{}
	validator := oidcValidatorFunc(func(_ context.Context, token, gotAudience string) (oidcIdentity, error) {
		if token != schedulerToken {
			return oidcIdentity{}, errors.New("invalid token")
		}
		if gotAudience != audience {
			return oidcIdentity{}, errors.New("invalid audience")
		}
		return oidcIdentity{
			Issuer:        googleServiceAccountIssuer,
			Email:         schedulerEmail,
			EmailVerified: true,
		}, nil
	})
	authorizer := newMaintenanceAuthorizer(audience, schedulerEmail, validator)
	router := NewRouter(NewHandlers(session, nil, nil), agentSecret, authorizer)

	tests := []struct {
		name   string
		bearer string
		status int
		calls  int
	}{
		{name: "missing credential", status: http.StatusUnauthorized},
		{name: "agent API credential", bearer: agentSecret, status: http.StatusUnauthorized},
		{name: "scheduler identity", bearer: schedulerToken, status: http.StatusNoContent, calls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session.maintainCalls = 0
			req := httptest.NewRequest(http.MethodPost, "/ops/session/maintenance", nil)
			if tt.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearer)
			}
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if session.maintainCalls != tt.calls {
				t.Fatalf("maintenance calls = %d, want %d", session.maintainCalls, tt.calls)
			}
			if session.getCalls != 0 {
				t.Fatalf("request called Session.Get %d times", session.getCalls)
			}
			if body := w.Body.String(); strings.Contains(body, schedulerToken) {
				t.Fatalf("response exposed scheduler token: %q", body)
			}
		})
	}
}

func TestMaintenanceRouteReturnsSafeFailure(t *testing.T) {
	session := &recordingMaintenanceSession{
		maintainErr: errors.New("login failed with token=provider-secret at https://provider.example.test"),
	}
	authorizer := newMaintenanceAuthorizer(
		"https://middleware.example.test",
		"middleware-maintenance@example.iam.gserviceaccount.com",
		oidcValidatorFunc(func(context.Context, string, string) (oidcIdentity, error) {
			return oidcIdentity{
				Issuer:        googleServiceAccountIssuer,
				Email:         "middleware-maintenance@example.iam.gserviceaccount.com",
				EmailVerified: true,
			}, nil
		}),
	)
	router := NewRouter(NewHandlers(session, nil, nil), "agent-api-secret", authorizer)
	req := httptest.NewRequest(http.MethodPost, "/ops/session/maintenance", nil)
	req.Header.Set("Authorization", "Bearer scheduler-id-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if session.maintainCalls != 1 {
		t.Fatalf("maintenance calls = %d, want 1", session.maintainCalls)
	}
	body := w.Body.String()
	for _, forbidden := range []string{"provider-secret", "provider.example.test", "login failed"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response %q exposed %q", body, forbidden)
		}
	}
}

func TestMaintenanceAuthorizerRejectsWrongOrUnverifiedIdentity(t *testing.T) {
	const (
		audience       = "https://middleware.example.test"
		schedulerEmail = "middleware-maintenance@example.iam.gserviceaccount.com"
	)
	tests := []struct {
		name     string
		identity oidcIdentity
	}{
		{
			name:     "wrong service account",
			identity: oidcIdentity{Issuer: googleServiceAccountIssuer, Email: "other@example.iam.gserviceaccount.com", EmailVerified: true},
		},
		{
			name:     "unverified email",
			identity: oidcIdentity{Issuer: googleServiceAccountIssuer, Email: schedulerEmail, EmailVerified: false},
		},
		{
			name:     "wrong issuer",
			identity: oidcIdentity{Issuer: "https://issuer.example.test", Email: schedulerEmail, EmailVerified: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authorizer := newMaintenanceAuthorizer(
				audience,
				schedulerEmail,
				oidcValidatorFunc(func(context.Context, string, string) (oidcIdentity, error) {
					return tt.identity, nil
				}),
			)
			req := httptest.NewRequest(http.MethodPost, "/ops/session/maintenance", nil)
			req.Header.Set("Authorization", "Bearer signed-google-token")

			if authorizer.Authorize(req) {
				t.Fatal("Authorize() = true, want false")
			}
		})
	}
}

type recordingMaintenanceSession struct {
	getCalls      int
	maintainCalls int
	maintainErr   error
}

func (s *recordingMaintenanceSession) Get(context.Context) (*domain.TokenData, error) {
	s.getCalls++
	return nil, nil
}

func (s *recordingMaintenanceSession) Maintain(context.Context) error {
	s.maintainCalls++
	return s.maintainErr
}

func (s *recordingMaintenanceSession) Status() session.SessionStatus {
	return session.SessionStatus{State: session.SessionUninitialized}
}

type oidcValidatorFunc func(context.Context, string, string) (oidcIdentity, error)

func (f oidcValidatorFunc) Validate(ctx context.Context, token, audience string) (oidcIdentity, error) {
	return f(ctx, token, audience)
}
