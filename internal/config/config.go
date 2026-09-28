package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	AdvancedMDUsername  string
	AdvancedMDPassword  string
	AdvancedMDOfficeKey string
	AdvancedMDAppName   string

	APISecret                     string
	BookingTokenSecret            string
	MaintenanceOIDCAudience       string
	MaintenanceOIDCServiceAccount string

	Port                string
	AllowRawSlotBooking bool
}

func Load() (*Config, error) {
	cfg := &Config{
		AdvancedMDUsername:            os.Getenv("ADVANCEDMD_USERNAME"),
		AdvancedMDPassword:            os.Getenv("ADVANCEDMD_PASSWORD"),
		AdvancedMDOfficeKey:           os.Getenv("ADVANCEDMD_OFFICE_KEY"),
		AdvancedMDAppName:             os.Getenv("ADVANCEDMD_APP_NAME"),
		APISecret:                     os.Getenv("API_SECRET"),
		BookingTokenSecret:            os.Getenv("BOOKING_TOKEN_SECRET"),
		MaintenanceOIDCAudience:       os.Getenv("MAINTENANCE_OIDC_AUDIENCE"),
		MaintenanceOIDCServiceAccount: os.Getenv("MAINTENANCE_OIDC_SERVICE_ACCOUNT"),
		Port:                          os.Getenv("PORT"),
		AllowRawSlotBooking:           parseBoolEnv(os.Getenv("ALLOW_RAW_SLOT_BOOKING")),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	if cfg.AdvancedMDUsername == "" {
		return nil, fmt.Errorf("ADVANCEDMD_USERNAME is required")
	}
	if cfg.AdvancedMDPassword == "" {
		return nil, fmt.Errorf("ADVANCEDMD_PASSWORD is required")
	}
	if cfg.AdvancedMDOfficeKey == "" {
		return nil, fmt.Errorf("ADVANCEDMD_OFFICE_KEY is required")
	}
	if cfg.AdvancedMDAppName == "" {
		return nil, fmt.Errorf("ADVANCEDMD_APP_NAME is required")
	}
	if cfg.APISecret == "" {
		return nil, fmt.Errorf("API_SECRET is required")
	}
	if err := validateMaintenanceAudience(cfg.MaintenanceOIDCAudience); err != nil {
		return nil, err
	}
	if !isServiceAccountEmail(cfg.MaintenanceOIDCServiceAccount) {
		return nil, fmt.Errorf("MAINTENANCE_OIDC_SERVICE_ACCOUNT must be a service account email")
	}
	if cfg.BookingTokenSecret == "" {
		cfg.BookingTokenSecret = cfg.APISecret
	}

	return cfg, nil
}

func validateMaintenanceAudience(value string) error {
	parsed, err := url.Parse(value)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Path != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return fmt.Errorf("MAINTENANCE_OIDC_AUDIENCE must be an HTTPS service base URL")
	}
	return nil
}

func isServiceAccountEmail(value string) bool {
	return strings.Count(value, "@") == 1 &&
		!strings.ContainsAny(value, " \t\r\n") &&
		strings.HasSuffix(value, ".iam.gserviceaccount.com")
}

func parseBoolEnv(value string) bool {
	switch value {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes", "on", "ON", "On":
		return true
	default:
		return false
	}
}
