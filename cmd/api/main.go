package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/config"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/eligibility"
	apphttp "advancedmd-token-management/internal/http"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/safelog"
	"advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

const version = "1.0.0"

func main() {
	// Configure logger to write to stdout (Railway interprets stderr as error-level)
	log.SetFlags(0)
	log.SetOutput(safelog.NewWriter(os.Stdout))
	log.Printf("Starting gateway v%s", version)

	// Initialize office registry based on AMD_ENV
	domain.InitRegistry(os.Getenv("AMD_ENV"))

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config category=%s", safeerrors.Classify(err))
	}

	// Initialize shared HTTP client for AdvancedMD calls
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			MaxConnsPerHost:     75,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Initialize the single owner for AdvancedMD authentication and token state.
	amdSession := session.NewSession(session.Credentials{
		Username:  cfg.AdvancedMDUsername,
		Password:  cfg.AdvancedMDPassword,
		OfficeKey: cfg.AdvancedMDOfficeKey,
		AppName:   cfg.AdvancedMDAppName,
	}, httpClient)

	// Initialize AdvancedMD XMLRPC client
	amdClient := clients.NewAdvancedMDClient(httpClient)

	// Initialize AdvancedMD REST client
	amdRestClient := clients.NewAdvancedMDRestClient(httpClient)

	// Compose the patient workflow over the domain-oriented AdvancedMD seam.
	patientRecords := advancedmd.NewAdapter(amdSession, amdClient, amdRestClient)
	appointmentTokens := scheduling.NewAppointmentTokens(cfg.BookingTokenSecret, time.Now)
	patients := patient.NewWithAppointmentTokens(patientRecords, appointmentTokens)
	scheduler := scheduling.NewWithConfig(
		patientRecords,
		cfg.BookingTokenSecret,
		time.Now,
		scheduling.Config{AllowRawBooking: cfg.AllowRawSlotBooking},
	)

	// Initialize handlers
	handlers := apphttp.NewHandlers(amdSession, patients, scheduler)
	// Eligibility remains off until practice providers and the secret are supplied.
	if key, providersJSON := os.Getenv("STEDI_API_KEY"), os.Getenv("STEDI_PROVIDERS"); key != "" || providersJSON != "" {
		var providers map[string]eligibility.Provider
		if json.Unmarshal([]byte(providersJSON), &providers) != nil {
			log.Fatal("invalid eligibility provider configuration")
		}
		service, err := eligibility.New(key, providers)
		if err != nil {
			log.Fatal("invalid eligibility configuration")
		}
		handlers.SetEligibility(service)
	}

	// Create router
	maintenanceAuthorizer := apphttp.NewMaintenanceAuthorizer(
		cfg.MaintenanceOIDCAudience,
		cfg.MaintenanceOIDCServiceAccount,
	)
	router := apphttp.NewRouter(handlers, cfg.APISecret, maintenanceAuthorizer)

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: session.DefaultSessionLoginTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error category=%s", safeerrors.Classify(err))
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown category=%s", safeerrors.Classify(err))
	}

	log.Println("Server exited")
}
