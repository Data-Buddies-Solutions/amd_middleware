package safeerrors

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
)

func TestClassifyReadsTheDeclaredCategoryNotTheMessage(t *testing.T) {
	sensitive := "patientId=17604634 token=secret-token provider-body=<private>"
	tests := []struct {
		name string
		err  error
		want Category
	}{
		{name: "no error", err: nil, want: CategoryNone},
		{name: "declared none", err: New(CategoryNone, "no category"), want: CategoryInternal},
		{name: "declared", err: New(CategoryUpstreamStatus, "status %s", sensitive), want: CategoryUpstreamStatus},
		{name: "declared and wrapped", err: fmt.Errorf("addpatient request failed: %w", New(CategoryConflict, "unexpected status 409")), want: CategoryConflict},
		{name: "undeclared conflict text", err: errors.New("conflict"), want: CategoryInternal},
		{name: "undeclared status text", err: errors.New("unexpected status 401"), want: CategoryInternal},
		{name: "undeclared transport text", err: errors.New("addpatient request failed: failed to marshal request"), want: CategoryInternal},
		{name: "undeclared response text", err: errors.New("lookuppatient returned malformed patientlist"), want: CategoryInternal},
		{name: "declared invalid response with misleading text", err: New(CategoryInvalidResponse, "request failed: 401 unauthorized connection"), want: CategoryInvalidResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyLetsContextAndTransportCausesOutrankDeclaredCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Category
	}{
		{name: "canceled", err: fmt.Errorf("request failed: %w", context.Canceled), want: categoryCanceled},
		{name: "deadline", err: fmt.Errorf("request failed: %w", context.DeadlineExceeded), want: CategoryTimeout},
		{name: "transport timeout", err: fmt.Errorf("request failed: %w", timeoutError{}), want: CategoryTimeout},
		{name: "transport failure", err: fmt.Errorf("request failed: %w", &net.OpError{Op: "dial", Err: errors.New("refused")}), want: CategoryNetwork},
		{name: "body read deadline", err: New(CategoryInvalidResponse, "failed to read response: %w", context.DeadlineExceeded), want: CategoryTimeout},
		{name: "body read reset", err: New(CategoryInvalidResponse, "failed to read response: %w", &net.OpError{Op: "read", Err: errors.New("reset")}), want: CategoryNetwork},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNotSentMarksARequestThatNeverReachedTheProvider(t *testing.T) {
	_, buildErr := http.NewRequest(http.MethodPost, "https://provider host/xmlrpc", nil)
	var netErr net.Error
	if !errors.As(buildErr, &netErr) {
		t.Fatal("request construction failure is expected to look like a transport error")
	}
	unsent := fmt.Errorf("addpatient request failed: %w", NotSent("failed to create request: %w", buildErr))
	if Classify(unsent) != CategoryInternal || !IsNotSent(unsent) {
		t.Fatalf("Classify() = %q, IsNotSent() = %t, want internal and not sent", Classify(unsent), IsNotSent(unsent))
	}
	for name, err := range map[string]error{
		"declared":   New(CategoryInternal, "unexpected provider state"),
		"undeclared": errors.New("failed to create request"),
		"transport":  fmt.Errorf("request failed: %w", context.DeadlineExceeded),
	} {
		if IsNotSent(err) {
			t.Errorf("%s: IsNotSent() = true, want false", name)
		}
	}
}

func TestDeclaredErrorsKeepTheirCauseAndMessage(t *testing.T) {
	cause := &detailError{code: "42"}
	err := New(CategoryRejected, "%w", cause)
	var detail *detailError
	if !errors.As(err, &detail) || detail.code != "42" {
		t.Fatalf("errors.As() did not reach the cause of %v", err)
	}
	if err.Error() != cause.Error() {
		t.Fatalf("Error() = %q, want %q", err.Error(), cause.Error())
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type detailError struct{ code string }

func (e *detailError) Error() string { return "rejected by provider" }
