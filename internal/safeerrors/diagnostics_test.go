package safeerrors

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticsBoundConcurrentProviderFailuresAndIsolateRequests(t *testing.T) {
	ctx, diagnostics := WithDiagnostics(context.Background())
	_, other := WithDiagnostics(context.Background())
	var workers sync.WaitGroup
	for i := 0; i < 100; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			Observe(ctx, ProviderDiagnostic{Operation: "get_appointments", Category: CategoryTimeout})
		}()
	}
	workers.Wait()
	failures, count := diagnostics.Snapshot()
	if count != 100 || len(failures) != maxProviderDiagnostics {
		t.Fatalf("count=%d retained=%d", count, len(failures))
	}
	failures[0].Operation = "changed"
	actual, _ := diagnostics.Snapshot()
	if actual[0].Operation == "changed" {
		t.Fatal("snapshot aliases collector")
	}
	if _, count := other.Snapshot(); count != 0 {
		t.Fatal("request diagnostics leaked")
	}
}

func TestDiagnosticsSummarizeConcurrentProviderCallsByOperation(t *testing.T) {
	ctx, diagnostics := WithDiagnostics(context.Background())
	_, other := WithDiagnostics(context.Background())
	var workers sync.WaitGroup
	for i := 1; i <= 100; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ObserveCall(ctx, "getdemographic", time.Duration(i)*time.Millisecond)
		}()
	}
	workers.Wait()
	ObserveCall(ctx, "book_appointment", 1500*time.Microsecond)
	want := []ProviderCallSummary{
		{Operation: "book_appointment", Calls: 1, TotalMS: 1, MaxMS: 1},
		{Operation: "getdemographic", Calls: 100, TotalMS: 5050, MaxMS: 100},
	}
	if got := diagnostics.Calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
	if got := other.Calls(); got != nil {
		t.Fatalf("other request calls = %+v, want none", got)
	}
}
