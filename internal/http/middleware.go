package http

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey contextKey = "requestID"
	// LogRequestIDKey is the context key for the redacted request ID used in logs.
	LogRequestIDKey contextKey = "logRequestID"
)

// AuthMiddleware validates the API secret in the Authorization header.
func AuthMiddleware(apiSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			expectedBearer := "Bearer " + apiSecret

			// Accept either "Bearer {secret}" or raw "{secret}"
			if auth != expectedBearer && auth != apiSecret {
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
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Capture status only. Request/response bodies may contain PHI.
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		requestID := GetLogRequestID(r.Context())

		log.Printf("[%s] %s %s %d %v",
			requestID,
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			duration,
		)
	})
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
