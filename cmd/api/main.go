package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"advancedmd-token-management/internal/auth"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/config"
	"advancedmd-token-management/internal/domain"
	apphttp "advancedmd-token-management/internal/http"
)

const version = "1.0.0"

func main() {
	// Configure logger to write to stdout (Railway interprets stderr as error-level)
	log.SetOutput(os.Stdout)
	log.Printf("Starting gateway v%s", version)

	// Initialize office registry based on AMD_ENV
	domain.InitRegistry(os.Getenv("AMD_ENV"))

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize shared HTTP client for AdvancedMD calls
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Initialize authenticator
	authenticator := auth.NewAuthenticator(auth.Credentials{
		Username:  cfg.AdvancedMDUsername,
		Password:  cfg.AdvancedMDPassword,
		OfficeKey: cfg.AdvancedMDOfficeKey,
		AppName:   cfg.AdvancedMDAppName,
	}, httpClient)

	// Initialize token manager
	tokenManager := auth.NewTokenManager(authenticator)

	// Start token manager (loads cache and starts background refresh)
	ctx := context.Background()
	if err := tokenManager.Start(ctx); err != nil {
		log.Fatalf("Failed to start token manager: %v", err)
	}
	defer tokenManager.Stop()
	log.Println("Token manager started")

	// Initialize AdvancedMD XMLRPC client
	amdClient := clients.NewAdvancedMDClient(httpClient)

	// Initialize AdvancedMD REST client
	amdRestClient := clients.NewAdvancedMDRestClient(httpClient)

	// Initialize handlers
	handlers := apphttp.NewHandlers(tokenManager, amdClient, amdRestClient, cfg.BookingTokenSecret)

	// Create router
	router := apphttp.NewRouter(handlers, cfg.APISecret)

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Server listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
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
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
