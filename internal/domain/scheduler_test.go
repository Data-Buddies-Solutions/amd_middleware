package domain

import (
	"slices"
	"testing"
	"time"
)

func TestSchedulerColumnSlotsFitWholeIntervalsInsideWorkHours(t *testing.T) {
	wednesday := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		start    string
		end      string
		interval int
		workweek int
		want     []string
	}{
		{name: "partial final interval", start: "08:00", end: "08:45", interval: 30, workweek: 127, want: []string{"08:00-08:30"}},
		{name: "exact intervals", start: "09:00", end: "10:00", interval: 20, workweek: 127, want: []string{"09:00-09:20", "09:20-09:40", "09:40-10:00"}},
		{name: "work period shorter than interval", start: "13:00", end: "13:15", interval: 30, workweek: 127},
		{name: "non-hour start", start: "14:10", end: "15:05", interval: 15, workweek: 127, want: []string{"14:10-14:25", "14:25-14:40", "14:40-14:55"}},
		{name: "not working that weekday", start: "09:00", end: "10:00", interval: 30, workweek: 127 &^ (1 << time.Wednesday)},
		{name: "zero interval", start: "09:00", end: "10:00", interval: 0, workweek: 127},
		{name: "negative interval", start: "09:00", end: "10:00", interval: -15, workweek: 127},
		{name: "invalid start time", start: "invalid", end: "17:00", interval: 15, workweek: 127},
		{name: "invalid end time", start: "09:00", end: "5pm", interval: 15, workweek: 127},
		{name: "end before start", start: "10:00", end: "09:00", interval: 15, workweek: 127},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			column := SchedulerColumn{StartTime: tt.start, EndTime: tt.end, Interval: tt.interval, Workweek: tt.workweek}
			var got []string
			for _, slot := range column.Slots(wednesday) {
				if slot.Start.Location() != time.UTC {
					t.Fatalf("slot %v is not tagged with the date's location", slot)
				}
				got = append(got, slot.Start.Format("15:04")+"-"+slot.End.Format("15:04"))
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("Slots() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntervalOverlapsIsHalfOpen(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 3, 3, hour, minute, 0, 0, time.UTC)
	}
	lunch := Interval{Start: at(12, 0), End: at(13, 0)}
	instant := Interval{Start: at(15, 0), End: at(15, 0)}
	tests := []struct {
		name  string
		slot  Interval
		other Interval
		want  bool
	}{
		{"ends at the other's start", Interval{at(11, 30), at(12, 0)}, lunch, false},
		{"bleeds into the other", Interval{at(11, 45), at(12, 15)}, lunch, true},
		{"same start", Interval{at(12, 0), at(12, 15)}, lunch, true},
		{"inside", Interval{at(12, 30), at(12, 45)}, lunch, true},
		{"covers the other", Interval{at(11, 0), at(14, 0)}, lunch, true},
		{"starts at the other's end", Interval{at(13, 0), at(13, 15)}, lunch, false},
		{"instant strictly inside", Interval{at(14, 45), at(15, 15)}, instant, true},
		{"instant at the start", Interval{at(15, 0), at(15, 30)}, instant, false},
		{"instant at the end", Interval{at(14, 30), at(15, 0)}, instant, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.slot.Overlaps(tt.other); got != tt.want {
				t.Fatalf("%v.Overlaps(%v) = %v, want %v", tt.slot, tt.other, got, tt.want)
			}
			if got := tt.other.Overlaps(tt.slot); got != tt.want {
				t.Fatalf("Overlaps is not symmetric for %v and %v", tt.slot, tt.other)
			}
		})
	}
}

func TestAppointmentIntervalEndsAfterItsDurationInMinutes(t *testing.T) {
	start := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	got := Appointment{StartDateTime: start, Duration: 45}.Interval()
	if !got.Equal(Interval{Start: start, End: start.Add(45 * time.Minute)}) {
		t.Fatalf("Interval() = %v", got)
	}
}
