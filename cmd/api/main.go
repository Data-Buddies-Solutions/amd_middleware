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
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/safelog"
	"advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

func main() {
	log.SetFlags(0)
	log.SetOutput(safelog.NewWriter(os.Stdout))
	log.Printf("Starting gateway")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	domain.InitRegistry(cfg.AMDEnv)

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
			MaxConnsPerHost:     75,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	product := clients.NewProductClient(httpClient, cfg.AcuityProductURL, cfg.AcuityProductInsuranceToken)
	if err := insurance.Refresh(context.Background(), product.ReadInsuranceCatalog); err != nil {
		log.Fatalf("Failed to install the insurance catalog from Product category=%s", safeerrors.Classify(err))
	}

	amdSession := session.NewSession(session.Credentials{
		Username:  cfg.AdvancedMDUsername,
		Password:  cfg.AdvancedMDPassword,
		OfficeKey: cfg.AdvancedMDOfficeKey,
		AppName:   cfg.AdvancedMDAppName,
	}, httpClient, time.Now)

	amdClient := clients.NewAdvancedMDClient(httpClient, time.Now)

	amdRestClient := clients.NewAdvancedMDRestClient(httpClient)

	patientRecords := advancedmd.NewAdapter(amdSession, amdClient, amdRestClient, time.Now)
	appointmentTokens := scheduling.NewAppointmentTokens(cfg.BookingTokenSecret, time.Now)
	patients := patient.New(patientRecords, appointmentTokens, time.Now)
	scheduler := scheduling.New(
		patientRecords,
		cfg.BookingTokenSecret,
		time.Now,
	)

	handlers := apphttp.NewHandlers(amdSession, patients, scheduler, time.Now)

	maintenanceAuthorizer := apphttp.NewMaintenanceAuthorizer(
		cfg.MaintenanceOIDCAudience,
		cfg.MaintenanceOIDCServiceAccount,
	)
	router := apphttp.NewRouter(handlers, cfg.APISecret, maintenanceAuthorizer)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: session.DefaultSessionLoginTimeout + 5*time.Second,
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
