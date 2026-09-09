package clients

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"advancedmd-token-management/internal/safeerrors"
)

func observeProviderFailure(ctx context.Context, operation string, started time.Time, err error) {
	if err == nil {
		return
	}
	diagnostic := safeerrors.ProviderDiagnostic{
		Operation: operation, Category: safeerrors.Classify(err), DurationMS: time.Since(started).Milliseconds(),
	}
	if observation, _ := ctx.Value(providerObservationKey{}).(*providerObservation); observation != nil {
		diagnostic.HTTPStatus = observation.httpStatus
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		diagnostic.HTTPStatus = status.StatusCode
	}
	var rejection *ProviderRejectionError
	if errors.As(err, &rejection) {
		diagnostic.Category = safeerrors.CategoryRejected
		diagnostic.Code = rejection.code
	}
	safeerrors.Observe(ctx, diagnostic)
}

var numericFaultCode = regexp.MustCompile(`^-?[0-9]{1,6}$`)

// Only short numeric values at the explicit faultcode path are kept.
// Free-form faults/descriptions may contain patient data and are never retained.
func providerRejection(operation string, body []byte) error {
	result := &ProviderRejectionError{operation: operation}
	var envelope struct {
		Results struct {
			Error struct {
				Fault struct {
					Code json.RawMessage `json:"faultcode"`
				} `json:"Fault"`
			} `json:"Error"`
		} `json:"PPMDResults"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return result
	}
	code := string(envelope.Results.Error.Fault.Code)
	var text string
	if json.Unmarshal(envelope.Results.Error.Fault.Code, &text) == nil {
		code = text
	}
	if numericFaultCode.MatchString(code) {
		result.code = code
	}
	return result
}

// The transport observes status before body reading/parsing can fail. Each
// semantic provider operation gets its own observation, including parallel reads.
type providerObservationKey struct{}
type providerObservation struct{ httpStatus int }

func beginProviderOperation(ctx context.Context, operation string) (context.Context, func(error)) {
	started := time.Now()
	ctx = context.WithValue(ctx, providerObservationKey{}, &providerObservation{})
	return ctx, func(err error) { observeProviderFailure(ctx, operation, started, err) }
}
func observeProviderStatus(ctx context.Context, status int) {
	if observation, _ := ctx.Value(providerObservationKey{}).(*providerObservation); observation != nil {
		observation.httpStatus = status
	}
}
