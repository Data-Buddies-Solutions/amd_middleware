package main

import (
	"context"
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
	apphttp "advancedmd-token-management/internal/http"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/safelog"
	"advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

const version = "1.0.0"

func main() {
	// Emit redacted structured logs to stdout.
	log.SetFlags(0)
	log.SetOutput(safelog.NewWriter(os.Stdout))
	log.Printf("Starting gateway v%s", version)

	offices := domain.NewOfficeCatalog(os.Getenv("AMD_ENV"))

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config category=%s", safeerrors.Classify(err))
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			MaxConnsPerHost:     75,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	amdSession := session.NewSession(session.Credentials{
		Username:  cfg.AdvancedMDUsername,
		Password:  cfg.AdvancedMDPassword,
		OfficeKey: cfg.AdvancedMDOfficeKey,
		AppName:   cfg.AdvancedMDAppName,
	}, httpClient)

	amdClient := clients.NewAdvancedMDClient(httpClient)

	amdRestClient := clients.NewAdvancedMDRestClient(httpClient)

	records := advancedmd.NewAdapter(offices, amdSession, amdClient, amdRestClient)
	appointmentTokens := scheduling.NewAppointmentTokens(offices, cfg.BookingTokenSecret, time.Now)
	patients := patient.NewWithAppointmentTokens(offices, records, appointmentTokens)
	scheduler := scheduling.NewWithConfig(
		offices,
		records,
		cfg.BookingTokenSecret,
		time.Now,
		scheduling.Config{AllowRawBooking: cfg.AllowRawSlotBooking},
	)

	handlers := apphttp.NewHandlers(offices, amdSession, patients, scheduler)

	maintenanceAuthorizer := apphttp.NewMaintenanceAuthorizer(
		cfg.MaintenanceOIDCAudience,
		cfg.MaintenanceOIDCServiceAccount,
	)
	router := apphttp.NewRouter(handlers, cfg.APISecret, maintenanceAuthorizer)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: apphttp.RequestTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Server listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error category=%s", safeerrors.Classify(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown category=%s", safeerrors.Classify(err))
	}

	log.Println("Server exited")
}
