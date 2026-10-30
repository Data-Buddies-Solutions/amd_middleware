package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"advancedmd-token-management/internal/domain"
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

	AcuityProductURL            string
	AcuityProductInsuranceToken string

	Port string

	AMDEnv domain.Environment
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
		AcuityProductURL:              os.Getenv("ACUITY_PRODUCT_URL"),
		AcuityProductInsuranceToken:   os.Getenv("ACUITY_PRODUCT_INSURANCE_TOKEN"),
		Port:                          os.Getenv("PORT"),
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
	env, err := domain.ParseEnvironment(os.Getenv("AMD_ENV"))
	if err != nil {
		return nil, fmt.Errorf("AMD_ENV: %w", err)
	}
	cfg.AMDEnv = env
	if err := validateMaintenanceAudience(cfg.MaintenanceOIDCAudience); err != nil {
		return nil, err
	}
	if !isServiceAccountEmail(cfg.MaintenanceOIDCServiceAccount) {
		return nil, fmt.Errorf("MAINTENANCE_OIDC_SERVICE_ACCOUNT must be a service account email")
	}
	if err := validateProductURL(cfg.AcuityProductURL); err != nil {
		return nil, err
	}
	if cfg.AcuityProductInsuranceToken == "" {
		return nil, fmt.Errorf("ACUITY_PRODUCT_INSURANCE_TOKEN is required")
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

func validateProductURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil ||
		!(parsed.Scheme == "https" || (parsed.Scheme == "http" && isLoopback(parsed.Hostname()))) ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Path != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return fmt.Errorf("ACUITY_PRODUCT_URL must be an HTTPS base URL, or an HTTP base URL on this host")
	}
	return nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func isServiceAccountEmail(value string) bool {
	return strings.Count(value, "@") == 1 &&
		!strings.ContainsAny(value, " \t\r\n") &&
		strings.HasSuffix(value, ".iam.gserviceaccount.com")
}
