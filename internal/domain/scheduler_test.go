package domain

import (
	"testing"
	"time"
)

func TestParseWorkHours_InvalidTime(t *testing.T) {
	date := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)

	col := &SchedulerColumn{
		StartTime: "invalid",
		EndTime:   "17:00",
	}

	_, _, err := col.ParseWorkHours(date)
	if err == nil {
		t.Error("Expected error for invalid start time")
	}
}
