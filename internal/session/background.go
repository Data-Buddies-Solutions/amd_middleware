package session

import (
	"context"
	"log"
	"sync"
	"time"

	"advancedmd-token-management/internal/safeerrors"
)

const backgroundMaintenanceInterval = 20 * time.Hour

// BackgroundMaintenance temporarily preserves the production refresh loop.
// Request-time correctness does not depend on this loop.
type BackgroundMaintenance struct {
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// StartBackgroundMaintenance starts the existing 20-hour refresh behavior.
func StartBackgroundMaintenance(session Session) *BackgroundMaintenance {
	ticker := time.NewTicker(backgroundMaintenanceInterval)
	return startBackgroundMaintenance(session, ticker.C, ticker.Stop)
}

func startBackgroundMaintenance(session Session, ticks <-chan time.Time, stopTicker func()) *BackgroundMaintenance {
	background := &BackgroundMaintenance{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(background.done)
		defer stopTicker()
		for {
			select {
			case <-background.stop:
				log.Println("Background session maintenance stopped")
				return
			case <-ticks:
				ctx, cancel := context.WithTimeout(context.Background(), DefaultSessionLoginTimeout)
				err := session.Maintain(ctx)
				cancel()
				if err != nil {
					log.Printf("Background session maintenance failed: category=%s", safeerrors.Classify(err))
				}
			}
		}
	}()
	return background
}

// Stop gracefully stops background session maintenance.
func (b *BackgroundMaintenance) Stop() {
	b.stopOnce.Do(func() {
		close(b.stop)
	})
	<-b.done
}
