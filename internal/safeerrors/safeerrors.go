package safeerrors

import (
	"context"
	"errors"
	"fmt"
	"net"
)

type Category string

const (
	CategoryNone            Category = "none"
	categoryCanceled        Category = "canceled"
	CategoryTimeout         Category = "timeout"
	CategoryNetwork         Category = "network"
	CategoryConflict        Category = "conflict"
	CategoryRejected        Category = "rejected"
	CategoryAuthentication  Category = "authentication"
	CategoryUnavailable     Category = "unavailable"
	CategoryUpstreamStatus  Category = "upstream_status"
	CategoryInvalidResponse Category = "invalid_response"
	CategoryInternal        Category = "internal"
	CategoryUpstreamError   Category = "upstream_error"
)

type categorized struct {
	category Category
	notSent  bool
	err      error
}

func (e *categorized) Error() string {
	return e.err.Error()
}

func (e *categorized) Unwrap() error {
	return e.err
}

func New(category Category, format string, args ...any) error {
	if category == CategoryNone {
		category = CategoryInternal
	}
	return &categorized{category: category, err: fmt.Errorf(format, args...)}
}

func NotSent(format string, args ...any) error {
	return &categorized{category: CategoryInternal, notSent: true, err: fmt.Errorf(format, args...)}
}

func IsNotSent(err error) bool {
	var declared *categorized
	return errors.As(err, &declared) && declared.notSent
}

func Classify(err error) Category {
	if err == nil {
		return CategoryNone
	}
	var declared *categorized
	isDeclared := errors.As(err, &declared)
	if isDeclared && declared.notSent {
		return declared.category
	}
	if errors.Is(err, context.Canceled) {
		return categoryCanceled
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
	if isDeclared {
		return declared.category
	}
	return CategoryInternal
}
