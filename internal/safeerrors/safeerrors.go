// Package safeerrors converts runtime errors into fixed, non-sensitive categories.
package safeerrors

import (
	"context"
	"errors"
	"net"
	"strings"
)

// Category is a stable label safe to include in logs.
type Category string

const (
	CategoryNone            Category = "none"
	CategoryCanceled        Category = "canceled"
	CategoryTimeout         Category = "timeout"
	CategoryNetwork         Category = "network"
	CategoryConflict        Category = "conflict"
	CategoryAuthentication  Category = "authentication"
	CategoryUpstreamStatus  Category = "upstream_status"
	CategoryInvalidResponse Category = "invalid_response"
	CategoryInternal        Category = "internal"
	CategoryUpstreamError   Category = "upstream_error"
)

// Classify converts an error into a safe category.
func Classify(err error) Category {
	if err == nil {
		return CategoryNone
	}
	if errors.Is(err, context.Canceled) {
		return CategoryCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CategoryTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return CategoryTimeout
		}
		return CategoryNetwork
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "conflict"):
		return CategoryConflict
	case containsAny(message, "401", "403", "unauthorized", "forbidden", "credential", "login failed", "no token"):
		return CategoryAuthentication
	case strings.Contains(message, "unexpected status"):
		return CategoryUpstreamStatus
	case containsAny(message, "parse", "malformed", "unexpected response", "read response"):
		return CategoryInvalidResponse
	case containsAny(message, "request failed", "send request", "connection", "network"):
		return CategoryNetwork
	case containsAny(message, "marshal", "create request", "not configured"):
		return CategoryInternal
	default:
		return CategoryUpstreamError
	}
}

func containsAny(message string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(message, value) {
			return true
		}
	}
	return false
}
