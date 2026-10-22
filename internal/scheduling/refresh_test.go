package scheduling_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

type gatedSetupRecords struct {
	*advancedmdtest.Adapter
	started chan struct{}
	release chan struct{}
}

func (r *gatedSetupRecords) GetSchedulerSetup(ctx context.Context) (domain.SchedulerSetup, error) {
	close(r.started)
	select {
	case <-r.release:
		return r.SchedulerSetup, nil
	case <-ctx.Done():
		return domain.SchedulerSetup{}, ctx.Err()
	}
}

func TestSetupWaiterCanCancelDuringRefresh(t *testing.T) {
	records := &gatedSetupRecords{Adapter: recordsWithSetup(), started: make(chan struct{}), release: make(chan struct{})}
	scheduler := scheduling.New(records, "secret", func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) })
	first := make(chan struct{})
	go func() { scheduler.List(context.Background(), scheduling.ListCommand{}); close(first) }()
	<-records.started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	waiter := make(chan error, 1)
	go func() { _, err := scheduler.List(ctx, scheduling.ListCommand{}); waiter <- err }()
	select {
	case err := <-waiter:
		if err == nil {
			t.Error("canceled waiter succeeded")
		}
	case <-time.After(time.Second):
		t.Error("canceled waiter blocked on network refresh")
	}
	close(records.release)
	<-first
}

func TestSchedulerSetupStaleFallbackHasMaximumAge(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "10:00", 15))
	scheduler := scheduling.New(records, "secret", func() time.Time { return now })
	command := scheduling.ListCommand{StartDate: "2026-06-10"}
	if _, err := scheduler.List(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24*time.Hour - 30*time.Second)
	records.SchedulerSetupError = errors.New("provider offline")
	if _, err := scheduler.List(context.Background(), command); err != nil {
		t.Fatalf("usable cached setup rejected: %v", err)
	}
	now = now.Add(30 * time.Second)
	if _, err := scheduler.List(context.Background(), command); err == nil {
		t.Fatal("day-old setup silently reused")
	}
}
