package config

import (
	"os"
	"testing"
)

// setEnvVars sets all required env vars for testing and returns a cleanup function.
func setEnvVars(t *testing.T) func() {
	t.Helper()
	vars := map[string]string{
		"ADVANCEDMD_USERNAME":   "testuser",
		"ADVANCEDMD_PASSWORD":   "testpass",
		"ADVANCEDMD_OFFICE_KEY": "991TEST",
		"ADVANCEDMD_APP_NAME":   "testapp",
		"API_SECRET":            "test-secret",
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
	if cfg.BookingTokenSecret != "test-secret" {
		t.Errorf("BookingTokenSecret = %q, want API secret fallback", cfg.BookingTokenSecret)
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

func TestLoad_DefaultPort(t *testing.T) {
	cleanup := setEnvVars(t)
	defer cleanup()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("Default port = %q, want '8080'", cfg.Port)
	}
}

func TestLoad_CustomPort(t *testing.T) {
	cleanup := setEnvVars(t)
	defer cleanup()
	os.Setenv("PORT", "3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Port != "3000" {
		t.Errorf("Port = %q, want '3000'", cfg.Port)
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
