package scheduling

import (
	"context"
	"fmt"
	"log"
	"time"

	"advancedmd-token-management/internal/domain"
)

const schedulerSetupMaxAge = 24 * time.Hour

type setupRefresh struct {
	done           chan struct{}
	setup          *domain.SchedulerSetup
	err            error
	callerCanceled bool
}

func (s *service) schedulerSetup(ctx context.Context, now time.Time) (*domain.SchedulerSetup, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.setupMu.Lock()
		if s.setup != nil && now.Before(s.setupExpiresAt) {
			setup := s.setup
			s.setupMu.Unlock()
			return setup, nil
		}
		if flight := s.setupFlight; flight != nil {
			s.setupMu.Unlock()
			select {
			case <-flight.done:
				if flight.callerCanceled {
					// A different request's cancellation must not fail this caller.
					now = s.now().UTC()
					continue
				}
				return flight.setup, flight.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		flight := &setupRefresh{done: make(chan struct{})}
		s.setupFlight = flight
		s.setupMu.Unlock()
		var setup domain.SchedulerSetup
		var err error
		if s.records == nil {
			err = fmt.Errorf("AdvancedMD scheduling records are not configured")
		} else {
			setup, err = s.records.GetSchedulerSetup(ctx)
		}
		s.setupMu.Lock()
		defer s.setupMu.Unlock()
		refreshedAt := s.now().UTC()
		if err == nil {
			s.setup = &setup
			s.setupLoadedAt = refreshedAt
			s.setupExpiresAt = refreshedAt.Add(schedulerSetupCacheTTL)
		} else if ctx.Err() == nil && s.setup != nil && refreshedAt.Sub(s.setupLoadedAt) < schedulerSetupMaxAge {
			log.Printf("WARNING: scheduler setup refresh failed; using cached setup category=%s", providerCategory(err))
			s.setupExpiresAt = refreshedAt.Add(time.Minute)
			if limit := s.setupLoadedAt.Add(schedulerSetupMaxAge); s.setupExpiresAt.After(limit) {
				s.setupExpiresAt = limit
			}
			err = nil
		}
		if err == nil {
			flight.setup = s.setup
		}
		flight.err = err
		flight.callerCanceled = err != nil && ctx.Err() != nil
		s.setupFlight = nil
		close(flight.done)
		return flight.setup, flight.err
	}
}
