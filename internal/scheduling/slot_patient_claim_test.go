package scheduling_test

import (
	"context"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

func twinRecords() *advancedmdtest.Adapter {
	records := patientStatusRecords()
	records.Demographics["67890"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "Self Pay", CarrierID: "car301672"}
	return records
}

func twinSlotToken(t *testing.T, svc scheduling.Scheduling, patientID string) string {
	t.Helper()
	available, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: patientID, Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical"})
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("availability: %v %+v", err, available)
	}
	return available.Slots[0].BookingToken
}

func TestSlotTokenSearchedForOnePatientCannotBookASameDOBPatient(t *testing.T) {
	records := twinRecords()
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	_, err := svc.Book(context.Background(), scheduling.BookCommand{
		PatientID: "67890", DOB: "01/15/1980", BookingToken: twinSlotToken(t, svc, "12345"),
		VisitCategory: "medical", AppointmentReason: "Blurry vision", ReferringDoctor: "none",
	})
	if got := scheduling.CategoryOf(err); err == nil || got != scheduling.CategoryPatientContextMismatch {
		t.Fatalf("book for the twin: err=%v category=%q", err, got)
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("wrote %d bookings for the twin", len(records.Bookings))
	}
}

func TestSlotTokenSearchedForOnePatientCannotMoveASameDOBPatient(t *testing.T) {
	records := twinRecords()
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	tokens := scheduling.NewAppointmentTokens("test-booking-secret", func() time.Time { return now })
	original := domain.PatientAppointment{
		ID: 54321, Start: time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC), AppointmentTypeID: 6169, OfficeID: "crystal_river",
	}
	records.AppointmentResults["67890"] = appointmentResult([]domain.PatientAppointment{original}, true)
	rescheduleToken, err := tokens.IssueRescheduleToken("67890", original)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Reschedule(context.Background(), scheduling.BookCommand{
		PatientID: "67890", DOB: "01/15/1980", BookingToken: twinSlotToken(t, svc, "12345"), RescheduleToken: rescheduleToken,
		VisitCategory: "medical", AppointmentReason: "Blurry vision", ReferringDoctor: "none",
	})
	if got := scheduling.CategoryOf(err); err == nil || got != scheduling.CategoryPatientContextMismatch {
		t.Fatalf("move for the twin: err=%v category=%q", err, got)
	}
	if len(records.Bookings) != 0 || len(records.Cancellations) != 0 {
		t.Fatalf("wrote %d bookings and %d cancellations for the twin", len(records.Bookings), len(records.Cancellations))
	}
}

func TestSlotTokenCarriesThePatientItWasSearchedFor(t *testing.T) {
	now := mutationTestNow()
	svc := scheduling.New(twinRecords(), "test-booking-secret", func() time.Time { return now })
	for raw, want := range map[string]domain.PatientID{"12345": "12345", "pat12345": "12345", " pat12345 ": "12345", "": ""} {
		policy, err := scheduling.VerifySlotToken("test-booking-secret", twinSlotToken(t, svc, raw), now)
		if err != nil || policy.PatientID != want {
			t.Errorf("patientId %q: policy patient %q, err %v", raw, policy.PatientID, err)
		}
	}
}

func TestSlotSearchRefusesAnUnparseablePatientIDBeforeAnyRead(t *testing.T) {
	records := twinRecords()
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	for _, raw := range []string{"abc", "+12345", "0123"} {
		available, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: raw, Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical"})
		if err == nil || scheduling.CategoryOf(err) != scheduling.CategoryValidation || len(available.Slots) != 0 {
			t.Errorf("patientId %q: outcome=%q slots=%d err=%v", raw, available.Outcome, len(available.Slots), err)
		}
	}
	if records.SchedulerSetupCalls != 0 {
		t.Errorf("refused searches read the scheduler %d times", records.SchedulerSetupCalls)
	}
}

func TestSlotTokenWithoutAPatientClaimStillBooksDuringRollout(t *testing.T) {
	records := twinRecords()
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	receipt, err := svc.Book(context.Background(), scheduling.BookCommand{
		PatientID: "67890", DOB: "01/15/1980", BookingToken: twinSlotToken(t, svc, ""),
		VisitCategory: "medical", AppointmentReason: "Blurry vision", ReferringDoctor: "none",
	})
	if err != nil || receipt.Status != "booked" || len(records.Bookings) != 1 {
		t.Fatalf("receipt=%+v err=%v bookings=%d", receipt, err, len(records.Bookings))
	}
}
