package scheduling

import (
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func TestAvailableSlotsExcludesSlotEndingAfterWorkHours(t *testing.T) {
	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	column := schedulingTestColumn("08:00", "08:45", 30)

	slots := availableSlots(schedulingPolicy{}, column, nil, nil, date, date.Add(-time.Hour))

	if len(slots) != 1 || slots[0].Time != "8:00 AM" {
		t.Fatalf("availableSlots() = %#v, want only 8:00 AM", slots)
	}
}

func TestAvailableSlotsAreSupportedByCurrentColumn(t *testing.T) {
	tests := []struct {
		name     string
		start    string
		end      string
		interval int
		want     []string
	}{
		{name: "partial final interval", start: "08:00", end: "08:45", interval: 30, want: []string{"8:00 AM"}},
		{name: "exact intervals", start: "09:00", end: "10:00", interval: 20, want: []string{"9:00 AM", "9:20 AM", "9:40 AM"}},
		{name: "work period shorter than interval", start: "13:00", end: "13:15", interval: 30, want: nil},
		{name: "non-hour start", start: "14:10", end: "15:05", interval: 15, want: []string{"2:10 PM", "2:25 PM", "2:40 PM"}},
	}

	date := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			column := schedulingTestColumn(tt.start, tt.end, tt.interval)
			slots := availableSlots(schedulingPolicy{}, column, nil, nil, date, date.Add(-time.Hour))
			if len(slots) != len(tt.want) {
				t.Fatalf("availableSlots() = %#v, want times %v", slots, tt.want)
			}
			for i, slot := range slots {
				if slot.Time != tt.want[i] {
					t.Errorf("slot %d time = %q, want %q", i, slot.Time, tt.want[i])
				}
				start, err := time.ParseInLocation(domain.SlotDateTimeLayout, slot.DateTime, date.Location())
				if err != nil {
					t.Fatalf("parse slot %q: %v", slot.DateTime, err)
				}
				if !currentColumnSupportsSlot(column, start, time.Duration(column.Interval)*time.Minute) {
					t.Errorf("currentColumnSupportsSlot() rejected available slot %q", slot.DateTime)
				}
			}
		})
	}
}

func schedulingTestColumn(start, end string, interval int) domain.SchedulerColumn {
	return domain.SchedulerColumn{
		StartTime: start,
		EndTime:   end,
		Interval:  interval,
		Workweek:  127,
	}
}
