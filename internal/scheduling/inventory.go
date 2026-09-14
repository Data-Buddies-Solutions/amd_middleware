package scheduling

import (
	"context"
	"sync"

	"advancedmd-token-management/internal/domain"
)

// readInventory bounds day-level concurrency. Each adapter read already fetches
// the day's eligible columns concurrently; never fan out the entire horizon.
func (s *service) readInventory(ctx context.Context, days []domain.ScheduleReadQuery) (map[string]domain.ScheduleReadResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queries := make(chan domain.ScheduleReadQuery)
	results := make(map[string]domain.ScheduleReadResult)
	var mu sync.Mutex
	var firstErr error
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for query := range queries {
				if ctx.Err() != nil {
					return
				}
				read, err := s.records.ReadSchedule(ctx, query)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
				} else {
					results[query.Date] = read
				}
				mu.Unlock()
			}
		}()
	}
dates:
	for _, query := range days {
		select {
		case queries <- query:
		case <-ctx.Done():
			break dates
		}
	}
	close(queries)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
