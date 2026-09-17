package scheduling_test

import (
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
	"context"
	"strings"
	"testing"
	"time"
)

func TestInsuranceRequirementsCannotBeBypassedByBookingRouting(t *testing.T) {
	for _, tc := range []struct{ name, plan, id string }{
		{"exchange portal referral", "United Healthcare Individual Exchange", "car40923"},
		{"global VOB", "United Healthcare Global", "car284971"},
		{"HUM02 authorization", "Humana Medicaid HMO", "car308175"},
		{"generic United", "United Healthcare", "car40923"},
		{"unknown carrier", "Unknown", "car999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := bookingRecords()
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: tc.plan, CarrierID: tc.id}
			now := mutationTestNow()
			command := signedBookCommand(t, now)
			command.Routing = "all_three"
			command.InsurancePlan = tc.plan
			command.AppointmentReason = "Hospital follow-up"
			command.HospitalName = "Example Hospital"
			command.HospitalDate = "2026-06-01"
			_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), command)
			if err == nil || len(records.Bookings) > 0 {
				t.Fatalf("booked %s without insurance clearance", tc.name)
			}
		})
	}
}

func TestPRE04CannotBookUncredentialedProvider(t *testing.T) {
	records := bookingRecords()
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "Preferred Care Partners", CarrierID: "car40916"}
	now := mutationTestNow()
	command := signedBookCommand(t, now)
	// A legacy raw request cannot broaden PRE04's credentialing.
	command.BookingToken = ""
	command.StartDatetime = "2026-06-03T09:00"
	command.Duration = 15
	command.ColumnID = 1551
	command.ProfileID = 621
	command.Routing = "all_three"
	command.Office = "Spring Hill"
	_, err := scheduling.NewWithConfig(records, "test-booking-secret", func() time.Time { return now }, scheduling.Config{AllowRawBooking: true}).Book(context.Background(), command)
	if err == nil || !strings.Contains(err.Error(), "provider") || len(records.Bookings) != 0 {
		t.Fatalf("err=%v writes=%d", err, len(records.Bookings))
	}
	command = signedBookCommand(t, now)
	if _, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func TestHospitalFollowUpRequiresHospitalAndDate(t *testing.T) {
	for _, tc := range []struct {
		hospital, date string
		missing        int
	}{{"", "", 2}, {"Example Hospital", "", 1}, {"", "June 1", 1}, {"Example Hospital", "June 1", 0}} {
		records := bookingRecords()
		now := mutationTestNow()
		command := signedBookCommand(t, now)
		command.AppointmentReason = "Hospital follow-up"
		command.HospitalName = tc.hospital
		command.HospitalDate = tc.date
		_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), command)
		if tc.missing > 0 {
			if err == nil || len(scheduling.MissingOf(err)) != tc.missing || len(records.Bookings) > 0 {
				t.Fatalf("missing=%d err=%v writes=%d", tc.missing, err, len(records.Bookings))
			}
		} else if err != nil {
			t.Fatal(err)
		} else if !strings.Contains(records.Bookings[0].Comments, "Example Hospital") || !strings.Contains(records.Bookings[0].Comments, "June 1") {
			t.Fatal("hospital details lost from appointment")
		}
	}
}

func TestPatientScopedInventoryRechecksChartInsurance(t *testing.T) {
	records := bookingRecords()
	now := mutationTestNow()
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "United Healthcare Individual Exchange", CarrierID: "car40923"}
	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Spring Hill", StartDate: "2026-06-03", DOB: "01/15/1980", Routing: "all_three", CoverageType: "medical"})
	if err == nil {
		t.Fatal("inventory bypassed referral and carrier verification")
	}
}
