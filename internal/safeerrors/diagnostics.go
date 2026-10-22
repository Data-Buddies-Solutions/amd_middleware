package safeerrors

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"
)

type ProviderDiagnostic struct {
	Operation  string   `json:"operation"`
	Category   Category `json:"category"`
	HTTPStatus int      `json:"httpStatus,omitempty"`
	Code       string   `json:"code,omitempty"`
	DurationMS int64    `json:"durationMs"`
}

type ProviderCallSummary struct {
	Operation string `json:"operation"`
	Calls     int    `json:"calls"`
	TotalMS   int64  `json:"totalMs"`
	MaxMS     int64  `json:"maxMs"`
}

const maxProviderDiagnostics = 8

type diagnosticKey struct{}

type providerCallTotals struct {
	calls   int
	total   time.Duration
	longest time.Duration
}

type Diagnostics struct {
	mu       sync.Mutex
	failures []ProviderDiagnostic
	count    int
	calls    map[string]providerCallTotals
}

func WithDiagnostics(ctx context.Context) (context.Context, *Diagnostics) {
	d := &Diagnostics{calls: map[string]providerCallTotals{}}
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

func ObserveCall(ctx context.Context, operation string, elapsed time.Duration) {
	d, _ := ctx.Value(diagnosticKey{}).(*Diagnostics)
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	totals := d.calls[operation]
	totals.calls++
	totals.total += elapsed
	totals.longest = max(totals.longest, elapsed)
	d.calls[operation] = totals
}

func (d *Diagnostics) Snapshot() ([]ProviderDiagnostic, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]ProviderDiagnostic(nil), d.failures...), d.count
}

func (d *Diagnostics) Calls() []ProviderCallSummary {
	d.mu.Lock()
	defer d.mu.Unlock()
	var summaries []ProviderCallSummary
	for _, operation := range slices.Sorted(maps.Keys(d.calls)) {
		totals := d.calls[operation]
		summaries = append(summaries, ProviderCallSummary{
			Operation: operation,
			Calls:     totals.calls,
			TotalMS:   totals.total.Milliseconds(),
			MaxMS:     totals.longest.Milliseconds(),
		})
	}
	return summaries
}
