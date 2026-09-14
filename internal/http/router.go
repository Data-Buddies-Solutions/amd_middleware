package http

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// RequestTimeout bounds the whole API workflow; the server leaves additional
// time to encode and deliver its outcome.
const RequestTimeout = 50 * time.Second

func requestDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// NewRouter creates and configures the HTTP router.
func NewRouter(handlers *Handlers, apiSecret string, maintenanceAuthorizer MaintenanceAuthorizer) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RealIP)
	r.Use(RequestIDMiddleware)
	r.Use(LoggingMiddleware(handlers.session))
	r.Use(recoveryMiddleware)

	// Process health checks (no auth required, no provider calls)
	r.Get("/health", handlers.HandleLive)
	r.Get("/live", handlers.HandleLive)
	r.Get("/ready", handlers.HandleReady)
	r.Get("/metrics", handlers.HandleMetrics)

	// Operational maintenance uses a dedicated Google-signed scheduler identity.
	r.With(MaintenanceAuthMiddleware(maintenanceAuthorizer)).
		Post("/ops/session/maintenance", handlers.HandleSessionMaintenance)

	// API routes (auth required)
	r.Route("/api", func(r chi.Router) {
		r.Use(AuthMiddleware(apiSecret))
		r.Use(requestDeadline)

		r.Post("/patient/resolve", handlers.HandlePatientResolve)
		r.Post("/add-patient", handlers.HandleAddPatient)
		r.Post("/scheduler/availability", handlers.HandleGetAvailability)
		r.Post("/scheduler/slots", handlers.HandleListAppointmentSlots)
		r.Post("/appointment/book", handlers.HandleBookAppointment)
		r.Post("/appointment/cancel", handlers.HandleCancelAppointment)
		r.Post("/patient/update-insurance", handlers.HandleUpdateInsurance)
	})

	return r
}
