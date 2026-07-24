package session

import (
	"context"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func TestBackgroundMaintenanceUsesSessionInterfaceAndStops(t *testing.T) {
	ticks := make(chan time.Time)
	maintained := make(chan struct{}, 1)
	session := &maintenanceSession{
		maintain: func(context.Context) error {
			maintained <- struct{}{}
			return nil
		},
	}
	background := startBackgroundMaintenance(session, ticks, func() {})

	ticks <- time.Now()
	select {
	case <-maintained:
	case <-time.After(time.Second):
		t.Fatal("background maintenance did not call Session.Maintain")
	}

	background.Stop()
	background.Stop()
}

type maintenanceSession struct {
	maintain func(context.Context) error
}

func (s *maintenanceSession) Get(context.Context) (*domain.TokenData, error) {
	return nil, nil
}

func (s *maintenanceSession) Maintain(ctx context.Context) error {
	return s.maintain(ctx)
}

func (s *maintenanceSession) Status() SessionStatus {
	return SessionStatus{State: SessionUninitialized}
}
