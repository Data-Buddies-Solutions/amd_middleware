package scheduling

import (
	"context"
	"time"

	"advancedmd-token-management/internal/domain"
)

func (s *service) readInventory(ctx context.Context, columns []domain.SchedulerColumn, start, end time.Time) (map[string]domain.ScheduleReadResult, error) {
	columnIDs := make([]string, 0, len(columns))
	for _, column := range columns {
		columnIDs = append(columnIDs, column.ID)
	}
	return s.records.ReadScheduleRange(ctx, domain.ScheduleRangeQuery{ColumnIDs: columnIDs, Start: start, End: end})
}
