package advancedmd

import (
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type budgetTransport func(*http.Request) (*http.Response, error)

func (f budgetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMutationLeavesOriginalContextForReconciliation(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	calls := 0
	client := &http.Client{Transport: budgetTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		got, ok := r.Context().Deadline()
		if !ok || !got.Equal(deadline.Add(-5*time.Second)) {
			t.Errorf("mutation deadline=%v", got)
		}
		return nil, context.DeadlineExceeded
	})}
	adapter := NewAdapter(domain.NewOfficeCatalog(""), staticSession{token: &domain.TokenData{RestApiBase: "provider.test"}}, nil, clients.NewAdvancedMDRestClient(client))
	err := adapter.CancelAppointment(ctx, Cancellation{AppointmentID: 1})
	if calls != 1 || !IsAmbiguousWrite(err) || ctx.Err() != nil {
		t.Fatalf("calls=%d error=%v parent=%v", calls, err, ctx.Err())
	}
}

func TestExhaustedMutationBudgetDoesNotSendWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	client := &http.Client{Transport: budgetTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") })}
	adapter := NewAdapter(domain.NewOfficeCatalog(""), staticSession{token: &domain.TokenData{RestApiBase: "provider.test"}}, nil, clients.NewAdvancedMDRestClient(client))
	err := adapter.CancelAppointment(ctx, Cancellation{AppointmentID: 1})
	if calls != 0 || IsAmbiguousWrite(err) || CategoryOf(err) != safeerrors.CategoryTimeout {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
