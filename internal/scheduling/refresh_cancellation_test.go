package scheduling_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

type canceledLeaderSetupRecords struct {
	*advancedmdtest.Adapter
	started chan struct{}
	calls   atomic.Int32
}

func (r *canceledLeaderSetupRecords) GetSchedulerSetup(ctx context.Context) (domain.SchedulerSetup, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
		<-ctx.Done()
		return domain.SchedulerSetup{}, ctx.Err()
	}
	return r.SchedulerSetup, nil
}

// Done is first observed when Search joins the existing setup refresh. This
// makes cancellation happen while another live request is actually waiting.
type observedSetupWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *observedSetupWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestSetupLiveWaiterRecoversAfterInitiatingRequestCancels(t *testing.T) {
	records := &canceledLeaderSetupRecords{
		Adapter: recordsWithSetup(), started: make(chan struct{}),
	}
	scheduler := scheduling.New(records, "secret", func() time.Time {
		return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	})
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	defer cancelLeader()
	leader := make(chan error, 1)
	go func() {
		_, err := scheduler.Search(leaderCtx, scheduling.SearchCommand{RequestedDate: "2026-06-10"})
		leader <- err
	}()
	select {
	case <-records.started:
	case <-time.After(time.Second):
		t.Fatal("initiating request never started setup refresh")
	}
	waitCtx, cancelWaiter := context.WithTimeout(context.Background(), time.Second)
	defer cancelWaiter()
	observed := &observedSetupWaitContext{Context: waitCtx, waiting: make(chan struct{})}
	waiter := make(chan error, 1)
	go func() {
		_, err := scheduler.Search(observed, scheduling.SearchCommand{RequestedDate: "2026-06-10"})
		waiter <- err
	}()
	select {
	case <-observed.waiting:
	case <-waitCtx.Done():
		t.Fatal("live request never joined setup refresh")
	}
	cancelLeader()
	select {
	case err := <-leader:
		if err == nil {
			t.Fatal("canceled initiating request unexpectedly succeeded")
		}
	case <-waitCtx.Done():
		t.Fatal("canceled initiating request did not finish")
	}
	select {
	case err := <-waiter:
		if err != nil {
			t.Fatalf("live search inherited another request's cancellation: %v", err)
		}
	case <-waitCtx.Done():
		t.Fatal("live search did not recover from canceled setup refresh")
	}
	if calls := records.calls.Load(); calls != 2 {
		t.Fatalf("setup calls = %d, want canceled attempt then live refresh", calls)
	}
}
