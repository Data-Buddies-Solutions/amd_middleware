package patient

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/safeerrors"
)

type MutationMetric struct {
	Operation string
	Outcome   string
	Count     uint64
}

type mutationMetricKey struct {
	operation string
	outcome   string
}

const (
	maxReadAttempts = 3
	readRetryDelay  = 5 * time.Millisecond
)

var mutationMetrics = struct {
	sync.Mutex
	counts map[mutationMetricKey]uint64
}{
	counts: make(map[mutationMetricKey]uint64),
}

func retryRead[T any](ctx context.Context, read func() (T, error)) (T, error) {
	var zero T
	for attempt := 1; attempt <= maxReadAttempts; attempt++ {
		result, err := read()
		if err == nil {
			return result, nil
		}
		if attempt == maxReadAttempts || !isTransientReadError(err) {
			return zero, err
		}

		if err := waitReadRetry(ctx, attempt); err != nil {
			return zero, err
		}
	}
	return zero, errors.New("patient read retry exhausted")
}

func waitReadRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(readRetryDelay * time.Duration(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientReadError(err error) bool {
	switch advancedmd.CategoryOf(err) {
	case safeerrors.CategoryTimeout, safeerrors.CategoryNetwork, safeerrors.CategoryUnavailable:
		return true
	default:
		return false
	}
}

func failureOutcome(err error) MutationOutcome {
	switch advancedmd.CategoryOf(err) {
	case safeerrors.CategoryUnavailable, safeerrors.CategoryAuthentication:
		return MutationUnavailable
	default:
		return MutationFailed
	}
}

func createOutcome(result CreateResult) string {
	if result.Outcome != "" {
		return string(result.Outcome)
	}
	if result.Status == CreateStatusCreated {
		return "success"
	}
	return "failed"
}

func updateInsuranceOutcome(result UpdateInsuranceResult) string {
	if result.Outcome != "" {
		return string(result.Outcome)
	}
	if result.Status == UpdateInsuranceStatusUpdated {
		return "success"
	}
	return "failed"
}

func recordMutation(operation, outcome string) {
	log.Printf("patient-mutation operation=%s category=%s", operation, outcome)

	mutationMetrics.Lock()
	defer mutationMetrics.Unlock()
	mutationMetrics.counts[mutationMetricKey{operation: operation, outcome: outcome}]++
}

func MutationMetricSnapshot() []MutationMetric {
	mutationMetrics.Lock()
	defer mutationMetrics.Unlock()

	snapshot := make([]MutationMetric, 0, len(mutationMetrics.counts))
	for key, count := range mutationMetrics.counts {
		snapshot = append(snapshot, MutationMetric{
			Operation: key.operation,
			Outcome:   key.outcome,
			Count:     count,
		})
	}
	sort.Slice(snapshot, func(i, j int) bool {
		if snapshot[i].Operation == snapshot[j].Operation {
			return snapshot[i].Outcome < snapshot[j].Outcome
		}
		return snapshot[i].Operation < snapshot[j].Operation
	})
	return snapshot
}
