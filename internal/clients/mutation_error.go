package clients

import (
	"errors"
	"net/http"
)

type MutationDisposition uint8

const (
	MutationDispositionUnknown MutationDisposition = iota
	MutationDispositionAuthentication
	MutationDispositionConflict
	MutationDispositionRejected
	MutationDispositionAmbiguous
)

type mutationError struct {
	disposition MutationDisposition
	cause       error
}

func (e *mutationError) Error() string {
	return e.cause.Error()
}

func (e *mutationError) Unwrap() error {
	return e.cause
}

func newMutationError(disposition MutationDisposition, cause error) error {
	return &mutationError{disposition: disposition, cause: cause}
}

func mutationDispositionForStatus(status int) MutationDisposition {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return MutationDispositionAuthentication
	case status == http.StatusConflict:
		return MutationDispositionConflict
	case status >= 400 && status < 500 && status != http.StatusRequestTimeout:
		return MutationDispositionRejected
	default:
		return MutationDispositionAmbiguous
	}
}

func MutationDispositionOf(err error) MutationDisposition {
	var mutationErr *mutationError
	if errors.As(err, &mutationErr) {
		return mutationErr.disposition
	}
	return MutationDispositionUnknown
}
