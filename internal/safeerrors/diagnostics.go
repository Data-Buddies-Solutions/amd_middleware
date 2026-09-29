package safeerrors

import (
	"context"
	"sync"
)

type ProviderDiagnostic struct {
	Operation  string   `json:"operation"`
	Category   Category `json:"category"`
	HTTPStatus int      `json:"httpStatus,omitempty"`
	Code       string   `json:"code,omitempty"`
	DurationMS int64    `json:"durationMs"`
}

const maxProviderDiagnostics = 8

type diagnosticKey struct{}

type Diagnostics struct {
	mu       sync.Mutex
	failures []ProviderDiagnostic
	count    int
}

func WithDiagnostics(ctx context.Context) (context.Context, *Diagnostics) {
	d := &Diagnostics{}
	return context.WithValue(ctx, diagnosticKey{}, d), d
}

func Observe(ctx context.Context, failure ProviderDiagnostic) {
	d, _ := ctx.Value(diagnosticKey{}).(*Diagnostics)
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++
	if len(d.failures) < maxProviderDiagnostics {
		d.failures = append(d.failures, failure)
	}
}

func (d *Diagnostics) Snapshot() ([]ProviderDiagnostic, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ProviderDiagnostic(nil), d.failures...), d.count
}
