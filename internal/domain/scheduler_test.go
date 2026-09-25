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

func TestIsBlockedByHold(t *testing.T) {
	holds := []BlockHold{
		{
			StartDateTime: time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC),
			EndDateTime:   time.Date(2026, 3, 3, 13, 0, 0, 0, time.UTC),
			Note:          "Lunch",
		},
		{
			StartDateTime: time.Date(2026, 3, 3, 15, 0, 0, 0, time.UTC),
			EndDateTime:   time.Date(2026, 3, 3, 15, 30, 0, 0, time.UTC),
			Note:          "Meeting",
		},
	}

	slot15 := 15 * time.Minute
	slot30 := 30 * time.Minute

	tests := []struct {
		name     string
		hour     int
		min      int
		duration time.Duration
		want     bool
	}{
		{"before lunch (15min)", 11, 30, slot15, false},
		{"15min slot ends at lunch start (no overlap)", 11, 45, slot15, false},
		{"30min slot ends at lunch start (no overlap)", 11, 30, slot30, false},
		{"15min slot bleeds into lunch", 11, 50, slot15, true},
		{"30min slot bleeds into lunch", 11, 45, slot30, true},
		{"start of lunch", 12, 0, slot15, true},
		{"during lunch", 12, 30, slot15, true},
		{"end of lunch (not blocked)", 13, 0, slot15, false},
		{"between holds", 14, 0, slot15, false},
		{"30min slot bleeds into meeting", 14, 45, slot30, true},
		{"during meeting", 15, 0, slot15, true},
		{"after meeting", 15, 30, slot15, false},
		{"morning slot", 9, 0, slot15, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slotTime := time.Date(2026, 3, 3, tt.hour, tt.min, 0, 0, time.UTC)
			got := IsBlockedByHold(slotTime, tt.duration, holds)
			if got != tt.want {
				t.Errorf("IsBlockedByHold(%02d:%02d, %v) = %v, want %v", tt.hour, tt.min, tt.duration, got, tt.want)
			}
		})
	}
}
