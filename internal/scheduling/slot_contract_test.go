package scheduling_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

const slotContractDate = "2026-06-03"

type slotContractToken struct {
	start    string
	duration int
	booked   int
	capacity int
	force    bool
}

func TestSlotContractListsAndRevalidatesRepresentativeSchedules(t *testing.T) {
	notWednesday := testColumn("1513", "620", "1568", "09:00", "10:00", 30)
	notWednesday.Workweek = 127 &^ (1 << time.Wednesday)
	tests := []struct {
		name         string
		office       string
		routing      domain.RoutingRule
		column       domain.SchedulerColumn
		appointments []domain.Appointment
		holds        []domain.BlockHold
		listed       []string
		rejected     []slotContractToken
	}{
		{
			name:    "final interval past work-hour end",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "08:00", "08:45", 30),
			listed:  []string{"08:00 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "08:30", duration: 30, capacity: 2},
				{start: "07:30", duration: 30, capacity: 2},
			},
		},
		{
			name:    "off-grid and wrong-length tokens",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "09:00", "10:00", 30),
			listed:  []string{"09:00 0/0 force=false", "09:30 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "09:10", duration: 30, capacity: 2},
				{start: "09:00", duration: 15, capacity: 2},
				{start: "09:00", duration: 60, capacity: 2},
			},
		},
		{
			name:    "slot crossing a block hold",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "09:00", "11:00", 30),
			holds:   []domain.BlockHold{{ID: 1, StartDateTime: slotContractAt(9, 55), EndDateTime: slotContractAt(10, 30)}},
			listed:  []string{"09:00 0/0 force=false", "10:30 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "09:30", duration: 30, capacity: 2},
				{start: "10:00", duration: 30, capacity: 2},
			},
		},
		{
			name:    "zero-length hold inside a slot",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "09:00", "10:00", 30),
			holds:   []domain.BlockHold{{ID: 1, StartDateTime: slotContractAt(9, 40), EndDateTime: slotContractAt(9, 40)}},
			listed:  []string{"09:00 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "09:30", duration: 30, capacity: 2},
			},
		},
		{
			name:    "hold carried over midnight from the previous day",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "09:00", "10:30", 30),
			holds: []domain.BlockHold{{
				ID:            1,
				StartDateTime: time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC),
				EndDateTime:   slotContractAt(9, 30),
			}},
			listed: []string{"09:30 0/0 force=false", "10:00 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "09:00", duration: 30, capacity: 2},
			},
		},
		{
			name:    "appointment durations overlapping other starts",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "08:30", "11:00", 30),
			appointments: []domain.Appointment{
				{ID: 1, StartDateTime: slotContractAt(8, 45), Duration: 15},
				{ID: 2, StartDateTime: slotContractAt(9, 30), Duration: 60},
				{ID: 3, StartDateTime: slotContractAt(10, 40), Duration: 0},
			},
			listed: []string{"09:00 0/0 force=false", "09:30 1/2 force=true"},
			rejected: []slotContractToken{
				{start: "08:30", duration: 30, capacity: 2},
				{start: "10:00", duration: 30, capacity: 2},
				{start: "10:30", duration: 30, capacity: 2},
			},
		},
		{
			name:    "same-start capacity of two",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  testColumn("1513", "620", "1568", "09:00", "10:30", 30),
			appointments: []domain.Appointment{
				{ID: 1, StartDateTime: slotContractAt(9, 0), Duration: 30},
				{ID: 2, StartDateTime: slotContractAt(9, 0), Duration: 30},
				{ID: 3, StartDateTime: slotContractAt(9, 30), Duration: 30},
			},
			listed: []string{"09:30 1/2 force=true", "10:00 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "09:00", duration: 30, booked: 1, capacity: 2, force: true},
				{start: "09:00", duration: 30, booked: 2, capacity: 2, force: false},
				{start: "09:30", duration: 30, booked: 0, capacity: 2, force: false},
				{start: "09:30", duration: 30, booked: 2, capacity: 2, force: true},
				{start: "10:00", duration: 30, booked: 1, capacity: 2, force: true},
			},
		},
		{
			name:    "same-start capacity of one",
			office:  "Crystal River",
			routing: domain.RoutingAll,
			column:  testColumn("1593", "2064", "1576", "09:00", "10:00", 30),
			appointments: []domain.Appointment{
				{ID: 1, StartDateTime: slotContractAt(9, 0), Duration: 30},
			},
			listed: []string{"09:30 0/0 force=false"},
			rejected: []slotContractToken{
				{start: "09:00", duration: 30, booked: 0, capacity: 1},
				{start: "09:00", duration: 30, booked: 1, capacity: 1},
			},
		},
		{
			name:    "column not working that weekday",
			office:  "Spring Hill",
			routing: domain.RoutingBachOnly,
			column:  notWednesday,
			listed:  nil,
			rejected: []slotContractToken{
				{start: "09:00", duration: 30, capacity: 2},
			},
		},
	}

	now := mutationTestNow()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := recordsWithSetup(tt.column)
			records.Demographics["12345"] = domain.PatientDemographics{CarrierName: "Self Pay", DOB: "01/15/1980"}
			records.Schedule[slotContractDate] = scheduleRows(tt.column.ID, tt.appointments, tt.holds)
			scheduler := scheduling.New(records, "test-booking-secret", func() time.Time { return now })

			result, err := scheduler.List(context.Background(), scheduling.ListCommand{
				StartDate: slotContractDate, Office: tt.office, Routing: string(tt.routing), DOB: "01/15/1980",
			})
			if err != nil {
				t.Fatalf("List error = %v", err)
			}
			var listed []string
			var slots []scheduling.AvailabilitySlotOption
			for _, slot := range result.Slots {
				if strings.HasPrefix(slot.DateTime, slotContractDate) {
					listed = append(listed, fmt.Sprintf("%s %d/%d force=%v", slot.DateTime[11:], slot.SameStartBooked, slot.SameStartCapacity, slot.RequiresForce))
					slots = append(slots, slot)
				}
			}
			if !slices.Equal(listed, tt.listed) {
				t.Fatalf("listed %v, want %v", listed, tt.listed)
			}

			for _, slot := range slots {
				before := len(records.Bookings)
				receipt, err := scheduler.Book(context.Background(), slotContractBooking(tt.office, slot.BookingToken))
				if err != nil || receipt.Status != "booked" || len(records.Bookings) != before+1 {
					t.Fatalf("listed slot %s: receipt=%#v error=%v", slot.DateTime, receipt, err)
				}
				booking := records.Bookings[before]
				if domain.FormatSlotDateTime(booking.Start) != slot.DateTime || booking.Duration != tt.column.Interval || booking.Force != slot.RequiresForce {
					t.Fatalf("listed slot %s booked as start=%s duration=%d force=%v", slot.DateTime, domain.FormatSlotDateTime(booking.Start), booking.Duration, booking.Force)
				}
			}

			office, err := domain.ResolveOffice(tt.office)
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range tt.rejected {
				signed := signSlotContractToken(t, now, office.ID, tt.routing, tt.column, token)
				_, err := scheduler.Book(context.Background(), slotContractBooking(tt.office, signed))
				if scheduling.CategoryOf(err) != scheduling.CategorySlotUnavailable {
					t.Fatalf("token %+v: error=%v category=%q, want slot_unavailable", token, err, scheduling.CategoryOf(err))
				}
			}
		})
	}
}

func slotContractAt(hour, minute int) time.Time {
	day, _ := time.Parse("2006-01-02", slotContractDate)
	return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

func slotContractBooking(office, token string) scheduling.BookCommand {
	return scheduling.BookCommand{
		PatientID: "12345", DOB: "01/15/1980", Office: office, BookingToken: token,
		VisitCategory:     domain.AppointmentVisitMedical,
		AppointmentReason: "Eye follow-up", ReferringDoctor: "none",
	}
}

func signSlotContractToken(t *testing.T, now time.Time, officeID string, routing domain.RoutingRule, column domain.SchedulerColumn, token slotContractToken) string {
	t.Helper()
	var columnID, profileID int
	fmt.Sscan(column.ID, &columnID)
	fmt.Sscan(column.ProfileID, &profileID)
	typeIDs := make([]int, 0, len(domain.AppointmentTypes))
	for typeID := range domain.AppointmentTypes {
		typeIDs = append(typeIDs, typeID)
	}
	slices.Sort(typeIDs)
	signed, err := scheduling.SignSlotToken("test-booking-secret", scheduling.SlotPolicy{
		OfficeID:           officeID,
		Routing:            routing,
		ColumnID:           columnID,
		ProfileID:          profileID,
		StartDatetime:      slotContractDate + "T" + token.start,
		Duration:           token.duration,
		DOB:                "01/15/1980",
		AppointmentTypeIDs: typeIDs,
		SameStartBooked:    token.booked,
		SameStartCapacity:  token.capacity,
		RequiresForce:      token.force,
		IssuedAt:           now.Unix(),
		ExpiresAt:          now.Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("SignSlotToken error = %v", err)
	}
	return signed
}
