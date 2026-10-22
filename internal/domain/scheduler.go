package domain

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

var easternLocation = loadEasternLocation()

func EasternLocation() *time.Location {
	return easternLocation
}

func loadEasternLocation() *time.Location {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(fmt.Sprintf("load America/New_York from the embedded tz database: %v", err))
	}
	return location
}

type SchedulerColumn struct {
	ID              string
	Name            string
	ProfileID       string
	FacilityID      string
	StartTime       string
	EndTime         string
	Interval        int
	MaxApptsPerSlot int
	Workweek        int
}

type SchedulerSetup struct {
	Columns []SchedulerColumn
}

type ScheduleRangeQuery struct {
	ColumnIDs []string
	Start     time.Time
	End       time.Time
}

type ColumnSchedule struct {
	Appointments         []Appointment
	BlockHolds           []BlockHold
	AppointmentsComplete bool
	BlockHoldsComplete   bool
}

func (s ColumnSchedule) Complete() bool {
	return s.AppointmentsComplete && s.BlockHoldsComplete
}

type ScheduleReadResult struct {
	Columns map[string]ColumnSchedule
}

type Appointment struct {
	ID            int
	StartDateTime time.Time
	Duration      int
	ColumnID      int
	ProfileID     int
	PatientID     int
}

type BlockHold struct {
	ID            int
	StartDateTime time.Time
	EndDateTime   time.Time
	ColumnID      int
}

type Interval struct {
	Start time.Time
	End   time.Time
}

func (i Interval) Overlaps(other Interval) bool {
	return i.Start.Before(other.End) && other.Start.Before(i.End)
}

func (i Interval) Equal(other Interval) bool {
	return i.Start.Equal(other.Start) && i.End.Equal(other.End)
}

func (a Appointment) Interval() Interval {
	return Interval{Start: a.StartDateTime, End: a.StartDateTime.Add(time.Duration(a.Duration) * time.Minute)}
}

func (h BlockHold) Interval() Interval {
	return Interval{Start: h.StartDateTime, End: h.EndDateTime}
}

func (c *SchedulerColumn) WorksOnDay(weekday time.Weekday) bool {
	bit := 1 << weekday
	return c.Workweek&bit != 0
}

func (c *SchedulerColumn) HasUsableSchedule() bool {
	_, startErr := time.Parse(workHoursLayout, c.StartTime)
	_, endErr := time.Parse(workHoursLayout, c.EndTime)
	return c.Interval > 0 && c.Workweek != 0 && startErr == nil && endErr == nil
}

const workHoursLayout = "15:04"

func (c *SchedulerColumn) Slots(date time.Time) []Interval {
	if !c.WorksOnDay(date.Weekday()) || c.Interval <= 0 {
		return nil
	}
	loc := date.Location()
	startTime, startErr := time.ParseInLocation(workHoursLayout, c.StartTime, loc)
	endTime, endErr := time.ParseInLocation(workHoursLayout, c.EndTime, loc)
	if startErr != nil || endErr != nil {
		return nil
	}
	workStart := time.Date(date.Year(), date.Month(), date.Day(), startTime.Hour(), startTime.Minute(), 0, 0, loc)
	workEnd := time.Date(date.Year(), date.Month(), date.Day(), endTime.Hour(), endTime.Minute(), 0, 0, loc)
	length := time.Duration(c.Interval) * time.Minute
	var slots []Interval
	for start := workStart; !start.Add(length).After(workEnd); start = start.Add(length) {
		slots = append(slots, Interval{Start: start, End: start.Add(length)})
	}
	return slots
}

const SlotDateTimeLayout = "2006-01-02T15:04"

func FormatSlotDateTime(t time.Time) string {
	return t.Format(SlotDateTimeLayout)
}
