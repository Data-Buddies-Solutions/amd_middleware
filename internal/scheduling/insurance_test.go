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
		{"HUM02 authorization", "Humana Medicaid HMO", "car308175"},
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
			_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), command)
			if err == nil || len(records.Bookings) > 0 {
				t.Fatalf("booked %s without insurance clearance", tc.name)
			}
		})
	}
}

func TestPRE04CannotBookUncredentialedProvider(t *testing.T) {
	records := recordsWithSetup(testColumn("1268", "620", "1480", "09:00", "09:15", 15))
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "Preferred Care Partners", CarrierID: "car40916"}
	records.ScheduleReads["2026-06-03"] = completeRead("1268", nil, nil)
	now := mutationTestNow()
	svc := scheduling.NewWithConfig(records, "test-booking-secret", func() time.Time { return now }, scheduling.Config{AllowRawBooking: true})
	// Caller routing cannot open an uncredentialed optical provider for PRE04.
	command := scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: "Hollywood", ColumnID: 1555, ProfileID: 2075, StartDatetime: "2026-06-03T09:00", Duration: 15, AppointmentTypeID: 1007, Routing: "all_three"}
	_, err := svc.Book(context.Background(), command)
	if err == nil || !strings.Contains(err.Error(), "provider") || len(records.Bookings) != 0 {
		t.Fatalf("err=%v writes=%d", err, len(records.Bookings))
	}
	command.ColumnID = 1268
	command.ProfileID = 620
	if _, err := svc.Book(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func TestHospitalFollowUpUsesOrdinaryBookingReason(t *testing.T) {
	for _, reason := range []string{"Hospital follow-up", "Hospital follow-up at Example Hospital on June 1", "Routine eye exam for a 9-year-old child"} {
		t.Run(reason, func(t *testing.T) {
			records := bookingRecords()
			now := mutationTestNow()
			command := signedBookCommand(t, now)
			command.AppointmentReason = reason
			_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), command)
			if err != nil || len(records.Bookings) != 1 {
				t.Fatalf("err=%v writes=%d", err, len(records.Bookings))
			}
			if !strings.Contains(strings.ToLower(records.Bookings[0].Comments), strings.ToLower(reason)) {
				t.Fatal("appointment reason was lost")
			}
		})
	}
}

func TestPatientScopedInventoryRechecksChartInsurance(t *testing.T) {
	records := bookingRecords()
	now := mutationTestNow()
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "United Healthcare", CarrierID: "car999"}
	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Spring Hill", StartDate: "2026-06-03", DOB: "01/15/1980", Routing: "all_three", CoverageType: "medical"})
	if err == nil {
		t.Fatal("inventory bypassed referral and carrier verification")
	}
}
