package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(handlers *Handlers, apiSecret string, maintenanceAuthorizer MaintenanceAuthorizer) http.Handler {
	r := chi.NewRouter()

	r.Use(requestIDMiddleware)
	r.Use(loggingMiddleware(handlers.session))
	r.Use(recoveryMiddleware)

	r.Get("/health", handlers.HandleLive)
	r.Get("/live", handlers.HandleLive)
	r.Get("/ready", handlers.HandleReady)
	r.Get("/metrics", handlers.HandleMetrics)

	r.With(maintenanceAuthMiddleware(maintenanceAuthorizer)).
		Post("/ops/session/maintenance", handlers.HandleSessionMaintenance)

	r.Route("/api", func(r chi.Router) {
		r.Use(authMiddleware(apiSecret))

		r.Post("/insurance/decision", handlers.HandleInsuranceDecision)
		r.Get("/insurance/plans", handlers.HandleInsurancePlans)
		r.Post("/patient/resolve", handlers.HandlePatientResolve)
		r.Post("/add-patient", handlers.HandleAddPatient)
		r.Post("/scheduler/availability", handlers.HandleGetAvailability)
		r.Post("/scheduler/slots", handlers.HandleListAppointmentSlots)
		r.Post("/appointment/reschedule", handlers.HandleRescheduleAppointment)
		r.Post("/appointment/book", handlers.HandleBookAppointment)
		r.Post("/appointment/cancel", handlers.HandleCancelAppointment)
		r.Post("/patient/update-insurance", handlers.HandleUpdateInsurance)
	})

	return r
}
