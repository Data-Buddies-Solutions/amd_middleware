package safeerrors

import (
	"context"
	"sync"
)

// ProviderDiagnostic contains only fixed operation/category labels, numeric
// status/fault codes, and timing. Never put provider messages or payloads here.
type ProviderDiagnostic struct {
	Operation  string   `json:"operation"`
	Category   Category `json:"category"`
	HTTPStatus int      `json:"httpStatus,omitempty"`
	Code       string   `json:"code,omitempty"`
	DurationMS int64    `json:"durationMs"`
}

const MaxProviderDiagnostics = 8

type diagnosticKey struct{}

// Diagnostics collects provider failures, including failures later reconciled
// successfully. The domain outcome remains the authority for the final result.
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
	if len(d.failures) < MaxProviderDiagnostics {
		d.failures = append(d.failures, failure)
	}
}

func (d *Diagnostics) Snapshot() ([]ProviderDiagnostic, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ProviderDiagnostic(nil), d.failures...), d.count
}
