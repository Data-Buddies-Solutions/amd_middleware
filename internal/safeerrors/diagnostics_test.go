package safeerrors

import (
	"context"
	"sync"
	"testing"
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
	if count != 100 || len(failures) != MaxProviderDiagnostics {
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
