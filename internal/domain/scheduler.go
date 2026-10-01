package domain

import (
	"fmt"
	"time"
)

var easternLocation = loadEasternLocation()

func EasternLocation() *time.Location {
	return easternLocation
}

func loadEasternLocation() *time.Location {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*60*60)
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

type SchedulerProfile struct {
	ID   string
	Code string
	Name string
}

type SchedulerFacility struct {
	ID   string
	Code string
	Name string
}

type SchedulerSetup struct {
	Columns    []SchedulerColumn
	Profiles   []SchedulerProfile
	Facilities []SchedulerFacility
}

type ScheduleReadQuery struct {
	ColumnIDs []string
	Date      string
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
	Note          string
}

func (c *SchedulerColumn) WorksOnDay(weekday time.Weekday) bool {
	bit := 1 << weekday
	return c.Workweek&bit != 0
}

func (c *SchedulerColumn) HasUsableSchedule() bool {
	_, _, err := c.ParseWorkHours(time.Now())
	return c.Interval > 0 && c.Workweek != 0 && err == nil
}

func (c *SchedulerColumn) ParseWorkHours(date time.Time) (start, end time.Time, err error) {
	loc := date.Location()

	startTime, err := time.ParseInLocation("15:04", c.StartTime, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start time: %w", err)
	}

	endTime, err := time.ParseInLocation("15:04", c.EndTime, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end time: %w", err)
	}

	start = time.Date(date.Year(), date.Month(), date.Day(),
		startTime.Hour(), startTime.Minute(), 0, 0, loc)
	end = time.Date(date.Year(), date.Month(), date.Day(),
		endTime.Hour(), endTime.Minute(), 0, 0, loc)

	return start, end, nil
}

const SlotDateTimeLayout = "2006-01-02T15:04"

func FormatSlotDateTime(t time.Time) string {
	return t.Format(SlotDateTimeLayout)
}
