package config

import (
	"os"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func setEnvVars(t *testing.T) func() {
	t.Helper()
	vars := map[string]string{
		"ADVANCEDMD_USERNAME":              "testuser",
		"ADVANCEDMD_PASSWORD":              "testpass",
		"ADVANCEDMD_OFFICE_KEY":            "991TEST",
		"ADVANCEDMD_APP_NAME":              "testapp",
		"API_SECRET":                       "test-secret",
		"MAINTENANCE_OIDC_AUDIENCE":        "https://middleware.example.test",
		"MAINTENANCE_OIDC_SERVICE_ACCOUNT": "middleware-maintenance@example.iam.gserviceaccount.com",
		"AMD_ENV":                          "prod",
		"ACUITY_PRODUCT_URL":               "https://product.example.test",
		"ACUITY_PRODUCT_INSURANCE_TOKEN":   "product-insurance-token",
	}

	for k, v := range vars {
		os.Setenv(k, v)
	}

	return func() {
		for k := range vars {
			os.Unsetenv(k)
		}
		os.Unsetenv("PORT")
		os.Unsetenv("BOOKING_TOKEN_SECRET")
	}
}

func TestLoad_Success(t *testing.T) {
	cleanup := setEnvVars(t)
	defer cleanup()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.AdvancedMDUsername != "testuser" {
		t.Errorf("AdvancedMDUsername = %q, want 'testuser'", cfg.AdvancedMDUsername)
	}
	if cfg.AdvancedMDPassword != "testpass" {
		t.Errorf("AdvancedMDPassword = %q, want 'testpass'", cfg.AdvancedMDPassword)
	}
	if cfg.AdvancedMDOfficeKey != "991TEST" {
		t.Errorf("AdvancedMDOfficeKey = %q, want '991TEST'", cfg.AdvancedMDOfficeKey)
	}
	if cfg.AdvancedMDAppName != "testapp" {
		t.Errorf("AdvancedMDAppName = %q, want 'testapp'", cfg.AdvancedMDAppName)
	}
	if cfg.APISecret != "test-secret" {
		t.Errorf("APISecret = %q, want 'test-secret'", cfg.APISecret)
	}
	if cfg.MaintenanceOIDCAudience != "https://middleware.example.test" {
		t.Errorf("MaintenanceOIDCAudience = %q", cfg.MaintenanceOIDCAudience)
	}
	if cfg.MaintenanceOIDCServiceAccount != "middleware-maintenance@example.iam.gserviceaccount.com" {
		t.Errorf("MaintenanceOIDCServiceAccount = %q", cfg.MaintenanceOIDCServiceAccount)
	}
	if cfg.BookingTokenSecret != "test-secret" {
		t.Errorf("BookingTokenSecret = %q, want API secret fallback", cfg.BookingTokenSecret)
	}
	if cfg.AMDEnv != domain.Prod {
		t.Errorf("AMDEnv = %q, want prod", cfg.AMDEnv)
	}
}

func TestLoad_AcceptsOnlyProdOrSandboxAMDEnv(t *testing.T) {
	cleanup := setEnvVars(t)
	defer cleanup()

	os.Setenv("AMD_ENV", "sandbox")
	cfg, err := Load()
	if err != nil || cfg.AMDEnv != domain.Sandbox {
		t.Fatalf("Load() with AMD_ENV=sandbox = (%+v, %v), want the sandbox environment", cfg, err)
	}
	for _, value := range []string{"", "dev", "production", "Prod", "staging"} {
		t.Run(value, func(t *testing.T) {
			os.Setenv("AMD_ENV", value)
			if cfg, err := Load(); err == nil {
				t.Fatalf("Load() with AMD_ENV=%q = %+v, want an error", value, cfg)
			}
		})
	}
}

func TestLoad_CustomBookingTokenSecret(t *testing.T) {
	cleanup := setEnvVars(t)
	defer cleanup()
	os.Setenv("BOOKING_TOKEN_SECRET", "booking-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.BookingTokenSecret != "booking-secret" {
		t.Errorf("BookingTokenSecret = %q, want 'booking-secret'", cfg.BookingTokenSecret)
	}
}

func TestLoad_MissingRequiredFields(t *testing.T) {
	requiredVars := []struct {
		name   string
		envVar string
	}{
		{"missing username", "ADVANCEDMD_USERNAME"},
		{"missing password", "ADVANCEDMD_PASSWORD"},
		{"missing office key", "ADVANCEDMD_OFFICE_KEY"},
		{"missing app name", "ADVANCEDMD_APP_NAME"},
		{"missing API secret", "API_SECRET"},
		{"missing maintenance OIDC audience", "MAINTENANCE_OIDC_AUDIENCE"},
		{"missing maintenance OIDC service account", "MAINTENANCE_OIDC_SERVICE_ACCOUNT"},
		{"missing AdvancedMD environment", "AMD_ENV"},
		{"missing Product URL", "ACUITY_PRODUCT_URL"},
		{"missing Product insurance token", "ACUITY_PRODUCT_INSURANCE_TOKEN"},
	}

	for _, tt := range requiredVars {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setEnvVars(t)
			defer cleanup()

			os.Unsetenv(tt.envVar)

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should fail when %s is missing", tt.envVar)
			}
		})
	}
}

func TestLoad_RejectsInvalidMaintenanceOIDCConfig(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		value  string
	}{
		{
			name:   "audience must use HTTPS",
			envVar: "MAINTENANCE_OIDC_AUDIENCE",
			value:  "http://middleware.example.test",
		},
		{
			name:   "audience must be a service base URL",
			envVar: "MAINTENANCE_OIDC_AUDIENCE",
			value:  "https://middleware.example.test/ops/session/maintenance",
		},
		{
			name:   "audience must not have a trailing slash",
			envVar: "MAINTENANCE_OIDC_AUDIENCE",
			value:  "https://middleware.example.test/",
		},
		{
			name:   "identity must be a service account",
			envVar: "MAINTENANCE_OIDC_SERVICE_ACCOUNT",
			value:  "scheduler@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setEnvVars(t)
			defer cleanup()
			os.Setenv(tt.envVar, tt.value)

			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want invalid configuration error")
			}
		})
	}
}

func TestLoad_ReadsTheProductInsuranceSource(t *testing.T) {
	for _, value := range []string{"https://product.example.test", "http://127.0.0.1:8090", "http://localhost:8090", "http://[::1]:8090"} {
		t.Run(value, func(t *testing.T) {
			cleanup := setEnvVars(t)
			defer cleanup()
			os.Setenv("ACUITY_PRODUCT_URL", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.AcuityProductURL != value || cfg.AcuityProductInsuranceToken != "product-insurance-token" {
				t.Fatalf("Product source = %q %q", cfg.AcuityProductURL, cfg.AcuityProductInsuranceToken)
			}
		})
	}
}

func TestLoad_RejectsAProductURLThatIsNotABaseURL(t *testing.T) {
	for _, value := range []string{
		"http://product.example.test",
		"https://product.example.test/",
		"https://product.example.test/v1/agent/insurance/read",
		"https://product.example.test?x=1",
		"https://user@product.example.test",
		"product.example.test",
	} {
		t.Run(value, func(t *testing.T) {
			cleanup := setEnvVars(t)
			defer cleanup()
			os.Setenv("ACUITY_PRODUCT_URL", value)

			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want invalid ACUITY_PRODUCT_URL")
			}
		})
	}
}
