package scheduling_test

import (
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
	"context"
	"strings"
	"testing"
	"time"
)

func TestExistingPatientBookingDoesNotRequireInsuranceClearance(t *testing.T) {
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
			if err != nil || len(records.Bookings) != 1 {
				t.Fatalf("existing patient blocked for %s: err=%v writes=%d", tc.name, err, len(records.Bookings))
			}
		})
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

func TestExistingPatientListsAndBooksWithoutInsuranceClarification(t *testing.T) {
	for _, plan := range []string{"AETNA", "Unknown", ""} {
		t.Run(plan, func(t *testing.T) {
			records := recordsWithSetup(testColumn("1593", "2064", "1576", "09:00", "09:15", 15))
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: plan, CarrierID: "car40887"}
			now := mutationTestNow()
			for i := 0; i < 14; i++ {
				day := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("2006-01-02")
				records.ScheduleReads[day] = completeRead("1593", nil, nil)
			}
			svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
			result, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", CoverageType: "medical", VisitType: "medical"})
			if err != nil || result.Outcome != domain.AvailabilityOutcomeFound || len(result.Slots) == 0 {
				t.Fatalf("existing patient inventory blocked: err=%v result=%+v", err, result)
			}
			if records.DemographicCalls != 0 {
				t.Fatal("inventory unnecessarily rechecked chart insurance")
			}
			booked, err := svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: "Crystal River", BookingToken: result.Slots[0].BookingToken, VisitCategory: "medical", PatientStatus: "established", AppointmentReason: "Eye follow-up", ReferringDoctor: "none"})
			if err != nil || booked.Status != "booked" || len(records.Bookings) != 1 {
				t.Fatalf("existing patient booking blocked: err=%v result=%+v writes=%d", err, booked, len(records.Bookings))
			}
		})
	}
}

func TestVisionCarrierIdentitySurvivesAvailabilityAndBooking(t *testing.T) {
	office, _ := domain.ResolveOffice("North Miami Beach Optical")
	accepted := domain.DecideInsurance("Devoted", "routine_vision", office, "01/15/1980")
	records := recordsWithSetup(testColumn("1601", "621", "1582", "09:00", "09:15", 15))
	records.SchedulerSetup.Profiles = append(records.SchedulerSetup.Profiles, domain.SchedulerProfile{ID: "621", Name: "BACH, MIRIAM"})
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierID: accepted.CarrierID, CarrierName: "PREMIER EYE CARE"}
	now := mutationTestNow()
	for i := 0; i < 14; i++ {
		day := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("2006-01-02")
		records.ScheduleReads[day] = completeRead("1601", nil, nil)
	}
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	available, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: office.DisplayName, DOB: "01/15/1980", InsurancePlan: accepted.CanonicalPlan, CoverageType: "routine_vision", VisitType: "routine_vision", StartDate: "2026-06-03"})
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("availability: %v %+v", err, available)
	}
	command := scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: office.DisplayName, InsurancePlan: accepted.CanonicalPlan, BookingToken: available.Slots[0].BookingToken, VisitCategory: "routine_vision", PatientStatus: "new", AppointmentReason: "Routine eye exam", ReferringDoctor: "none"}
	// A directory-label change does not create a new insurance gate.
	chart := records.Demographics["12345"]
	chart.CarrierName = "Renamed directory label"
	records.Demographics["12345"] = chart
	result, err := svc.Book(context.Background(), command)
	if err != nil || result.Status != "booked" || len(records.Bookings) != 1 {
		t.Fatalf("booking: %v %+v writes=%d", err, result, len(records.Bookings))
	}
}
