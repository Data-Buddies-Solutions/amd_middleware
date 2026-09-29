package http

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
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

type contextKey string

const (
	logRequestIDKey contextKey = "logRequestID"
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
	RequestID          string                          `json:"request_id"`
	RouteTemplate      string                          `json:"route_template"`
	Outcome            outcomeCategory                 `json:"outcome_category"`
	LatencyMS          int64                           `json:"latency_ms"`
	SessionState       session.SessionState            `json:"session_state"`
	ProviderFailure    safeerrors.Category             `json:"provider_failure_category"`
	Cancellation       *cancellationLogEntry           `json:"cancellation,omitempty"`
	PatientResolution  *patientResolutionLogEntry      `json:"patient_resolution,omitempty"`
	ProviderErrors     []safeerrors.ProviderDiagnostic `json:"provider_errors,omitempty"`
	ProviderErrorCount int                             `json:"provider_error_count,omitempty"`
	HTTPStatus         int                             `json:"http_status"`
}

var requestLogMu sync.Mutex

func authMiddleware(apiSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := []byte(r.Header.Get("Authorization"))

			bearerMatch := subtle.ConstantTimeCompare(auth, []byte("Bearer "+apiSecret))
			rawMatch := subtle.ConstantTimeCompare(auth, []byte(apiSecret))
			if bearerMatch|rawMatch != 1 {
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

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		logRequestID := requestID
		if requestID == "" {
			requestID = uuid.New().String()
			logRequestID = requestID
		} else if parsed, err := uuid.Parse(requestID); err != nil || parsed.String() != requestID || parsed.Version() != 4 {
			digest := sha256.Sum256([]byte(requestID))
			logRequestID = fmt.Sprintf("external-%x", digest[:8])
		}

		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), logRequestIDKey, logRequestID)))
	})
}

func loggingMiddleware(amdSession session.Session) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			state := &requestLogState{providerFailure: safeerrors.CategoryNone}
			ctx := context.WithValue(r.Context(), requestLogKey, state)
			ctx, diagnostics := safeerrors.WithDiagnostics(ctx)

			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK, state: state, diagnostics: diagnostics}

			next.ServeHTTP(wrapped, r.WithContext(ctx))

			if state.outcome == "" {
				state.outcome = outcomeForStatus(wrapped.statusCode)
			}
			routeTemplate := chi.RouteContext(r.Context()).RoutePattern()
			if routeTemplate == "" {
				routeTemplate = "unmatched"
			}
			providerErrors, providerErrorCount := diagnostics.Snapshot()
			writeRequestLog(requestLogEntry{
				RequestID:          requestIDForLog(r.Context()),
				RouteTemplate:      routeTemplate,
				Outcome:            state.outcome,
				LatencyMS:          time.Since(start).Milliseconds(),
				SessionState:       requestSessionState(amdSession),
				ProviderFailure:    state.providerFailure,
				Cancellation:       state.cancellation,
				PatientResolution:  state.patientResolve,
				ProviderErrors:     providerErrors,
				ProviderErrorCount: providerErrorCount,
				HTTPStatus:         wrapped.statusCode,
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
			w.Header().Set("Content-Type", "application/json")
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

type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
	state       *requestLogState
	diagnostics *safeerrors.Diagnostics
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	outcome := rw.state.outcome
	if outcome == "" {
		outcome = outcomeForStatus(code)
	}
	rw.Header().Set("X-Abita-Outcome", string(outcome))
	rw.Header().Set("X-Abita-Error-Category", string(rw.state.providerFailure))
	if failures, count := rw.diagnostics.Snapshot(); count > 0 {
		encoded, _ := json.Marshal(failures)
		rw.Header().Set("X-Abita-Provider-Errors", string(encoded))
		rw.Header().Set("X-Abita-Provider-Error-Count", fmt.Sprint(count))
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func requestIDForLog(ctx context.Context) string {
	if id, ok := ctx.Value(logRequestIDKey).(string); ok {
		return id
	}
	return ""
}
