package patient

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
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

func mutationLabel(outcome MutationOutcome, succeeded bool) string {
	switch {
	case outcome != "":
		return string(outcome)
	case succeeded:
		return "success"
	default:
		return "failed"
	}
}

func subscriberNumber(plan, subscriberNum string) string {
	if insurance.IsSelfPayInsurance(plan) && strings.TrimSpace(subscriberNum) == "" {
		return "self pay"
	}
	return subscriberNum
}

func decidePlan(plan, coverage string, office *domain.OfficeConfig, dob string) insurance.InsuranceDecision {
	if coverage == "" {
		coverage = "medical"
	}
	return insurance.DecideInsurance(plan, coverage, office, dob)
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
