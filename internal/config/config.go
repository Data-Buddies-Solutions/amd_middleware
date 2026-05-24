// Package config handles loading and validating environment variables.
package config

import (
	"fmt"
	"os"
)

// Config holds all configuration values for the application.
type Config struct {
	// AdvancedMD credentials
	AdvancedMDUsername  string
	AdvancedMDPassword  string
	AdvancedMDOfficeKey string
	AdvancedMDAppName   string

	// API authentication
	APISecret          string
	BookingTokenSecret string

	// Server settings
	Port                string
	AllowRawSlotBooking bool
	AllowLegacyCancel   bool
}

// Load reads configuration from environment variables and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		AdvancedMDUsername:  os.Getenv("ADVANCEDMD_USERNAME"),
		AdvancedMDPassword:  os.Getenv("ADVANCEDMD_PASSWORD"),
		AdvancedMDOfficeKey: os.Getenv("ADVANCEDMD_OFFICE_KEY"),
		AdvancedMDAppName:   os.Getenv("ADVANCEDMD_APP_NAME"),
		APISecret:           os.Getenv("API_SECRET"),
		BookingTokenSecret:  os.Getenv("BOOKING_TOKEN_SECRET"),
		Port:                os.Getenv("PORT"),
		AllowRawSlotBooking: parseBoolEnv(os.Getenv("ALLOW_RAW_SLOT_BOOKING")),
		AllowLegacyCancel:   parseBoolEnv(os.Getenv("ALLOW_LEGACY_CANCEL")),
	}

	// Default port
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	// Validate required fields
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
	if cfg.BookingTokenSecret == "" {
		cfg.BookingTokenSecret = cfg.APISecret
	}

	return cfg, nil
}

func parseBoolEnv(value string) bool {
	switch value {
	case "1", "true", "TRUE", "True", "yes", "YES", "Yes", "on", "ON", "On":
		return true
	default:
		return false
	}
}
