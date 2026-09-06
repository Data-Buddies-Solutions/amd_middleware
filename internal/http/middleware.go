package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	patientmodule "advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey contextKey = "requestID"
	// LogRequestIDKey is the context key for the redacted request ID used in logs.
	LogRequestIDKey contextKey = "logRequestID"
	requestLogKey   contextKey = "requestLog"
)

type outcomeCategory string

const (
	outcomeSuccess                outcomeCategory = "success"
	outcomeInvalidRequest         outcomeCategory = "invalid_request"
	outcomeAuthenticationRejected outcomeCategory = "authentication_rejected"
	outcomeProviderFailure        outcomeCategory = "provider_failure"
	outcomeInternalFailure        outcomeCategory = "internal_failure"
	outcomeNotFound               outcomeCategory = "not_found"
	outcomeClientError            outcomeCategory = "client_error"
	outcomeServerError            outcomeCategory = "server_error"
)

type requestLogState struct {
	outcome         outcomeCategory
	providerFailure safeerrors.Category
	cancellation    *cancellationLogEntry
	patientResolve  *patientResolutionLogEntry
}

type cancellationLogEntry struct {
	Path              string `json:"path"`
	Outcome           string `json:"outcome"`
	ScheduleReads     int    `json:"schedule_reads"`
	ProviderMutations int    `json:"provider_mutations"`
	DurationMS        int64  `json:"duration_ms"`
}

type patientResolutionLogEntry struct {
	PatientSearchDurationMS int64  `json:"patient_search_duration_ms"`
	DemographicDurationMS   int64  `json:"demographic_duration_ms"`
	AppointmentDurationMS   int64  `json:"appointment_duration_ms"`
	PatientSearchReads      int    `json:"patient_search_reads"`
	DemographicReads        int    `json:"demographic_reads"`
	AppointmentReads        int    `json:"appointment_reads"`
	ProviderReads           int    `json:"provider_reads"`
	OfficeGroupSize         int    `json:"office_group_size"`
	CandidateCount          string `json:"candidate_count"`
	AppointmentOutcome      string `json:"appointment_outcome"`
}

type requestLogEntry struct {
	RequestID         string                     `json:"request_id"`
	RouteTemplate     string                     `json:"route_template"`
	Outcome           outcomeCategory            `json:"outcome_category"`
	LatencyMS         int64                      `json:"latency_ms"`
	SessionState      session.SessionState       `json:"session_state"`
	ProviderFailure   safeerrors.Category        `json:"provider_failure_category"`
	Cancellation      *cancellationLogEntry      `json:"cancellation,omitempty"`
	PatientResolution *patientResolutionLogEntry `json:"patient_resolution,omitempty"`
}

var requestLogMu sync.Mutex

// AuthMiddleware validates the API secret in the Authorization header.
func AuthMiddleware(apiSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			expectedBearer := "Bearer " + apiSecret

			// Accept either "Bearer {secret}" or raw "{secret}"
			if auth != expectedBearer && auth != apiSecret {
				recordRequestOutcome(r.Context(), outcomeAuthenticationRejected, safeerrors.CategoryNone)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"Unauthorized"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMiddleware adds a unique request ID to each request.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		logRequestID := requestID
		if requestID == "" {
			requestID = uuid.New().String()
			logRequestID = requestID
		} else {
			digest := sha256.Sum256([]byte(requestID))
			logRequestID = fmt.Sprintf("external-%x", digest[:8])
		}

		// Add to response header
		w.Header().Set("X-Request-ID", requestID)

		// Add to context
		ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
		ctx = context.WithValue(ctx, LogRequestIDKey, logRequestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs request details and duration.
func LoggingMiddleware(amdSession session.Session) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			state := &requestLogState{providerFailure: safeerrors.CategoryNone}
			ctx := context.WithValue(r.Context(), requestLogKey, state)

			// Capture status only. Request/response bodies may contain PHI.
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r.WithContext(ctx))

			if state.outcome == "" {
				state.outcome = outcomeForStatus(wrapped.statusCode)
			}
			routeTemplate := chi.RouteContext(r.Context()).RoutePattern()
			if routeTemplate == "" {
				routeTemplate = "unmatched"
			}
			writeRequestLog(requestLogEntry{
				RequestID:         GetLogRequestID(r.Context()),
				RouteTemplate:     routeTemplate,
				Outcome:           state.outcome,
				LatencyMS:         time.Since(start).Milliseconds(),
				SessionState:      requestSessionState(amdSession),
				ProviderFailure:   state.providerFailure,
				Cancellation:      state.cancellation,
				PatientResolution: state.patientResolve,
			})
		})
	}
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() == nil {
				return
			}
			recordRequestOutcome(r.Context(), outcomeInternalFailure, safeerrors.CategoryNone)
			w.WriteHeader(http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

func requestSessionState(amdSession session.Session) session.SessionState {
	if amdSession == nil {
		return session.SessionUninitialized
	}
	return amdSession.Status().State
}

func recordRequestOutcome(ctx context.Context, outcome outcomeCategory, providerFailure safeerrors.Category) {
	state, ok := ctx.Value(requestLogKey).(*requestLogState)
	if !ok {
		return
	}
	state.outcome = outcome
	state.providerFailure = providerFailure
}

func recordCancellationObservation(
	ctx context.Context,
	observation schedulingmodule.CancellationObservation,
) {
	state, ok := ctx.Value(requestLogKey).(*requestLogState)
	if !ok {
		return
	}
	state.cancellation = &cancellationLogEntry{
		Path:              observation.Path,
		Outcome:           observation.Outcome,
		ScheduleReads:     observation.ScheduleReads,
		ProviderMutations: observation.ProviderMutations,
		DurationMS:        observation.DurationMS,
	}
}

func recordPatientResolutionObservation(
	ctx context.Context,
	observation patientmodule.ResolutionObservation,
) {
	if !observation.Recorded {
		return
	}
	state, ok := ctx.Value(requestLogKey).(*requestLogState)
	if !ok {
		return
	}
	state.patientResolve = &patientResolutionLogEntry{
		PatientSearchDurationMS: observation.PatientSearchDurationMS,
		DemographicDurationMS:   observation.DemographicDurationMS,
		AppointmentDurationMS:   observation.AppointmentDurationMS,
		PatientSearchReads:      observation.PatientSearchReads,
		DemographicReads:        observation.DemographicReads,
		AppointmentReads:        observation.AppointmentReads,
		ProviderReads: observation.PatientSearchReads +
			observation.DemographicReads +
			observation.AppointmentReads,
		OfficeGroupSize:    observation.OfficeGroupSize,
		CandidateCount:     observation.CandidateCountBucket,
		AppointmentOutcome: observation.AppointmentOutcome,
	}
}

func outcomeForStatus(status int) outcomeCategory {
	switch {
	case status == http.StatusNotFound:
		return outcomeNotFound
	case status >= http.StatusInternalServerError:
		return outcomeServerError
	case status >= http.StatusBadRequest:
		return outcomeClientError
	default:
		return outcomeSuccess
	}
}

func writeRequestLog(entry requestLogEntry) {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	encoded = append(encoded, '\n')
	requestLogMu.Lock()
	defer requestLogMu.Unlock()
	_, _ = log.Writer().Write(encoded)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

// GetRequestID retrieves the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetLogRequestID retrieves the redacted request ID from the context.
func GetLogRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(LogRequestIDKey).(string); ok {
		return id
	}
	return ""
}
