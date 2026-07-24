package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates and configures the HTTP router.
func NewRouter(handlers *Handlers, apiSecret string) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware)

	// Process health checks (no auth required, no provider calls)
	r.Get("/health", handlers.HandleLive)
	r.Get("/live", handlers.HandleLive)
	r.Get("/ready", handlers.HandleReady)

	// API routes (auth required)
	r.Route("/api", func(r chi.Router) {
		r.Use(AuthMiddleware(apiSecret))

		r.Post("/patient/resolve", handlers.HandlePatientResolve)
		r.Post("/add-patient", handlers.HandleAddPatient)
		r.Post("/scheduler/availability", handlers.HandleGetAvailability)
		r.Post("/appointment/book", handlers.HandleBookAppointment)
		r.Post("/appointment/cancel", handlers.HandleCancelAppointment)
		r.Post("/patient/update-insurance", handlers.HandleUpdateInsurance)
	})

	return r
}
