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

	// Health check (no auth required)
	r.Get("/health", handlers.HandleHealth)

	// API routes (auth required)
	r.Route("/api", func(r chi.Router) {
		r.Use(AuthMiddleware(apiSecret))

		r.Post("/verify-patient", handlers.HandleVerifyPatient)
		r.Post("/patient-lookup", handlers.HandlePatientLookup)
		r.Post("/add-patient", handlers.HandleAddPatient)
		r.Post("/scheduler/availability", handlers.HandleGetAvailability)
		r.Post("/patient/appointments", handlers.HandleGetPatientAppointments)
		r.Post("/patient/notes", handlers.HandleAddPatientNote)
		r.Post("/appointment/book", handlers.HandleBookAppointment)
		r.Post("/appointment/cancel", handlers.HandleCancelAppointment)
		r.Post("/patient/update-insurance", handlers.HandleUpdateInsurance)
	})

	return r
}
