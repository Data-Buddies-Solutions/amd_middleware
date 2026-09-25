package config

import (
	"os"
	"testing"
)

// setEnvVars sets all required env vars for testing and returns a cleanup function.
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
		os.Unsetenv("ALLOW_RAW_SLOT_BOOKING")
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
	if cfg.AllowRawSlotBooking {
		t.Error("AllowRawSlotBooking should default to false")
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

func TestLoad_LegacyFlags(t *testing.T) {
	cleanup := setEnvVars(t)
	defer cleanup()
	os.Setenv("ALLOW_RAW_SLOT_BOOKING", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if !cfg.AllowRawSlotBooking {
		t.Error("AllowRawSlotBooking = false, want true")
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
	}

	for _, tt := range requiredVars {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setEnvVars(t)
			defer cleanup()

			// Unset the one we're testing
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
