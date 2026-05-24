package domain

import (
	"testing"
	"time"
)

func TestWorksOnDay(t *testing.T) {
	// Mon-Fri: bitmask = 2+4+8+16+32 = 62
	monFri := &SchedulerColumn{Workweek: 62}
	// Wed-Thu: bitmask = 8+16 = 24
	wedThu := &SchedulerColumn{Workweek: 24}

	tests := []struct {
		name    string
		col     *SchedulerColumn
		weekday time.Weekday
		want    bool
	}{
		{"Mon-Fri: Monday", monFri, time.Monday, true},
		{"Mon-Fri: Friday", monFri, time.Friday, true},
		{"Mon-Fri: Saturday", monFri, time.Saturday, false},
		{"Mon-Fri: Sunday", monFri, time.Sunday, false},
		{"Wed-Thu: Wednesday", wedThu, time.Wednesday, true},
		{"Wed-Thu: Thursday", wedThu, time.Thursday, true},
		{"Wed-Thu: Monday", wedThu, time.Monday, false},
		{"Wed-Thu: Friday", wedThu, time.Friday, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.col.WorksOnDay(tt.weekday)
			if got != tt.want {
				t.Errorf("WorksOnDay(%v) = %v, want %v", tt.weekday, got, tt.want)
			}
		})
	}
}

func TestParseWorkHours(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	date := time.Date(2026, 3, 3, 0, 0, 0, 0, eastern) // Tuesday

	col := &SchedulerColumn{
		StartTime: "08:00",
		EndTime:   "17:00",
	}

	start, end, err := col.ParseWorkHours(date)
	if err != nil {
		t.Fatalf("ParseWorkHours failed: %v", err)
	}

	if start.Hour() != 8 || start.Minute() != 0 {
		t.Errorf("Expected start 08:00, got %02d:%02d", start.Hour(), start.Minute())
	}
	if end.Hour() != 17 || end.Minute() != 0 {
		t.Errorf("Expected end 17:00, got %02d:%02d", end.Hour(), end.Minute())
	}
	if start.Year() != 2026 || start.Month() != 3 || start.Day() != 3 {
		t.Errorf("Start date wrong: %v", start)
	}
}

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

func TestFormatSlotTime(t *testing.T) {
	tests := []struct {
		hour, min int
		want      string
	}{
		{9, 0, "9:00 AM"},
		{9, 30, "9:30 AM"},
		{12, 0, "12:00 PM"},
		{13, 0, "1:00 PM"},
		{17, 0, "5:00 PM"},
		{8, 15, "8:15 AM"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			slot := time.Date(2026, 3, 3, tt.hour, tt.min, 0, 0, time.UTC)
			got := FormatSlotTime(slot)
			if got != tt.want {
				t.Errorf("FormatSlotTime(%02d:%02d) = %q, want %q", tt.hour, tt.min, got, tt.want)
			}
		})
	}
}

func TestFormatSlotDateTime(t *testing.T) {
	slot := time.Date(2026, 3, 3, 14, 30, 0, 0, time.UTC)
	got := FormatSlotDateTime(slot)
	want := "2026-03-03T14:30"
	if got != want {
		t.Errorf("FormatSlotDateTime() = %q, want %q", got, want)
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

func TestIsBlockedByHold_EmptyHolds(t *testing.T) {
	slotTime := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	got := IsBlockedByHold(slotTime, 15*time.Minute, nil)
	if got {
		t.Error("Expected false for nil holds")
	}
	got = IsBlockedByHold(slotTime, 15*time.Minute, []BlockHold{})
	if got {
		t.Error("Expected false for empty holds")
	}
}

func TestIsAllowedColumn(t *testing.T) {
	office := DefaultOffice()

	tests := []struct {
		columnID string
		want     bool
	}{
		{"1513", true},
		{"1551", true},
		{"1550", true},
		{"9999", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.columnID, func(t *testing.T) {
			got := office.IsAllowedColumn(tt.columnID)
			if got != tt.want {
				t.Errorf("IsAllowedColumn(%q) = %v, want %v", tt.columnID, got, tt.want)
			}
		})
	}
}
