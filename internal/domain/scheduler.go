package domain

import (
	"fmt"
	"time"
)

// SchedulerColumn represents a provider's scheduling column from getschedulersetup.
// A column is a provider + location combination with specific work hours.
type SchedulerColumn struct {
	ID              string // Column ID (e.g., "1716")
	Name            string // Display name (e.g., "DR. BACH - BP")
	ProfileID       string // Provider profile ID (e.g., "1135")
	FacilityID      string // Facility ID (e.g., "fac1032")
	StartTime       string // Work start time (e.g., "08:00")
	EndTime         string // Work end time (e.g., "17:00")
	Interval        int    // Slot interval in minutes (e.g., 15)
	MaxApptsPerSlot int    // Max appointments per slot (0 = unlimited)
	Workweek        int    // Bitmask for working days (1=Sun, 2=Mon, 4=Tue, etc.)
}

// SchedulerProfile represents a provider profile from getschedulersetup.
type SchedulerProfile struct {
	ID   string // Profile ID (e.g., "1135")
	Code string // Provider code (e.g., "ABCH")
	Name string // Provider name (e.g., "BACH, AUSTIN")
}

// SchedulerFacility represents a facility/location from getschedulersetup.
type SchedulerFacility struct {
	ID   string // Facility ID (e.g., "fac1032")
	Code string // Facility code (e.g., "ABSPR")
	Name string // Facility name (e.g., "ABITA EYE GROUP SPRING HILL")
}

// SchedulerSetup holds the complete scheduler configuration.
type SchedulerSetup struct {
	Columns    []SchedulerColumn
	Profiles   []SchedulerProfile
	Facilities []SchedulerFacility
}

// Appointment represents a booked appointment from the REST API.
type Appointment struct {
	ID            int       // Appointment ID
	StartDateTime time.Time // Appointment start time
	Duration      int       // Duration in minutes
	ColumnID      int       // Column ID
	ProfileID     int       // Profile ID
	PatientID     int       // Patient ID
}

// BlockHold represents a blocked time period from the REST API.
type BlockHold struct {
	ID            int       // Block hold ID
	StartDateTime time.Time // Block start time
	EndDateTime   time.Time // Block end time (from AMD enddatetime)
	ColumnID      int       // Column ID
	Note          string    // Optional note (e.g., "Lunch")
}

// AvailableSlot represents a single available time slot.
type AvailableSlot struct {
	Time     string `json:"time"`     // Human-readable time (e.g., "9:00 AM")
	DateTime string `json:"datetime"` // ISO format for booking (e.g., "2026-02-03T09:00")
}

const (
	AvailabilityStatusSuccess = "success"
	AvailabilityStatusError   = "error"

	AvailabilityOutcomeFound               = "availability_found"
	AvailabilityOutcomeNoAvailability      = "no_availability"
	AvailabilityOutcomeNoEligibleProviders = "no_eligible_providers"
	AvailabilityOutcomeSearchIncomplete    = "availability_search_incomplete"

	AvailabilityNextActionOfferSlots                  = "offer_slots"
	AvailabilityNextActionAskDifferentPreferences     = "ask_for_different_preferences"
	AvailabilityNextActionRetryOnceThenAskPreferences = "retry_once_then_ask_preferences"
)

// ProviderAvailability represents an internal provider-level availability result.
type ProviderAvailability struct {
	Name           string          `json:"name"`
	ColumnID       int             `json:"columnId"`
	ProfileID      int             `json:"profileId"`
	Facility       string          `json:"facility"`
	SlotDuration   int             `json:"slotDuration"`
	TotalAvailable int             `json:"totalAvailable"`
	FirstAvailable string          `json:"firstAvailable,omitempty"`
	LastAvailable  string          `json:"lastAvailable,omitempty"`
	Slots          []AvailableSlot `json:"slots"`
}

// AvailabilitySlotOption is a single bookable slot returned to the agent.
type AvailabilitySlotOption struct {
	Provider  string `json:"provider"`
	Time      string `json:"time"`
	DateTime  string `json:"datetime"`
	ColumnID  int    `json:"columnId"`
	ProfileID int    `json:"profileId"`
	Duration  int    `json:"duration"`
}

// AvailabilityResponse is the response structure for the availability endpoint.
type AvailabilityResponse struct {
	Status                string                   `json:"status"`
	Outcome               string                   `json:"outcome"`
	AvailabilityFound     bool                     `json:"availabilityFound"`
	RequestedDate         string                   `json:"requestedDate,omitempty"`
	ActualDate            string                   `json:"actualDate,omitempty"`
	DateShifted           bool                     `json:"dateShifted,omitempty"`
	SearchedFrom          string                   `json:"searchedFrom,omitempty"`
	SearchedThrough       string                   `json:"searchedThrough,omitempty"`
	ShouldRetrySameSearch bool                     `json:"shouldRetrySameSearch"`
	NextAction            string                   `json:"nextAction"`
	Message               string                   `json:"message,omitempty"`
	Slots                 []AvailabilitySlotOption `json:"slots"`
}

// WorksOnDay checks if the column works on a given weekday.
// Weekday: 0=Sunday, 1=Monday, ..., 6=Saturday
// Workweek bitmask: 1=Sun, 2=Mon, 4=Tue, 8=Wed, 16=Thu, 32=Fri, 64=Sat
func (c *SchedulerColumn) WorksOnDay(weekday time.Weekday) bool {
	bit := 1 << weekday
	return c.Workweek&bit != 0
}

// ParseWorkHours parses start and end times into time values for a given date.
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

// FormatSlotTime formats a time for the AvailableSlot response.
func FormatSlotTime(t time.Time) string {
	return t.Format("3:04 PM")
}

// FormatSlotDateTime formats a time for ISO booking format.
func FormatSlotDateTime(t time.Time) string {
	return t.Format("2006-01-02T15:04")
}

// IsBlockedByHold checks if a time slot overlaps any block hold.
// A slot is blocked if [slotStart, slotStart+duration) overlaps [holdStart, holdEnd).
func IsBlockedByHold(slotTime time.Time, slotDuration time.Duration, holds []BlockHold) bool {
	slotEnd := slotTime.Add(slotDuration)
	for _, hold := range holds {
		// Two intervals overlap when each starts before the other ends
		if slotTime.Before(hold.EndDateTime) && slotEnd.After(hold.StartDateTime) {
			return true
		}
	}
	return false
}
