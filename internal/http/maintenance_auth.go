package http

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/api/idtoken"
)

const googleServiceAccountIssuer = "https://accounts.google.com"

type MaintenanceAuthorizer interface {
	Authorize(*http.Request) bool
}

type oidcIdentity struct {
	Issuer        string
	Email         string
	EmailVerified bool
}

type oidcValidator interface {
	Validate(context.Context, string, string) (oidcIdentity, error)
}

type googleOIDCValidator struct{}

func (googleOIDCValidator) Validate(ctx context.Context, token, audience string) (oidcIdentity, error) {
	payload, err := idtoken.Validate(ctx, token, audience)
	if err != nil {
		return oidcIdentity{}, err
	}
	email, _ := payload.Claims["email"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	return oidcIdentity{
		Issuer:        payload.Issuer,
		Email:         email,
		EmailVerified: emailVerified,
	}, nil
}

type maintenanceAuthorizer struct {
	audience       string
	serviceAccount string
	validator      oidcValidator
}

func NewMaintenanceAuthorizer(audience, serviceAccount string) MaintenanceAuthorizer {
	return newMaintenanceAuthorizer(audience, serviceAccount, googleOIDCValidator{})
}

func newMaintenanceAuthorizer(audience, serviceAccount string, validator oidcValidator) *maintenanceAuthorizer {
	return &maintenanceAuthorizer{
		audience:       audience,
		serviceAccount: serviceAccount,
		validator:      validator,
	}
}

func (a *maintenanceAuthorizer) Authorize(r *http.Request) bool {
	const bearerPrefix = "Bearer "
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, bearerPrefix) {
		return false
	}
	token := strings.TrimPrefix(authorization, bearerPrefix)
	if token == "" {
		return false
	}

	identity, err := a.validator.Validate(r.Context(), token, a.audience)
	if err != nil {
		return false
	}
	return identity.Issuer == googleServiceAccountIssuer &&
		identity.EmailVerified &&
		identity.Email == a.serviceAccount
}

func maintenanceAuthMiddleware(authorizer MaintenanceAuthorizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authorizer == nil || !authorizer.Authorize(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"Unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
