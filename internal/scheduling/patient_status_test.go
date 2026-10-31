package scheduling_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/scheduling"
)

func patientStatusRecords() *advancedmdtest.Adapter {
	records := recordsWithSetup(testColumn("1593", "2064", "1576", "09:00", "09:15", 15))
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "Self Pay", CarrierID: "car301672"}
	return records
}

func bookMedicalAtCrystalRiver(t *testing.T, records *advancedmdtest.Adapter) (scheduling.BookReceipt, error) {
	t.Helper()
	now := mutationTestNow()
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	available, err := svc.List(context.Background(), scheduling.ListCommand{PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical"})
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("availability: %v %+v", err, available)
	}
	return svc.Book(context.Background(), scheduling.BookCommand{
		PatientID: "12345", DOB: "01/15/1980", Office: "Crystal River", BookingToken: available.Slots[0].BookingToken,
		VisitCategory: "medical", AppointmentReason: "Blurry vision", ReferringDoctor: "none",
	})
}

func TestBookingClassifiesPatientFromCompletedVisits(t *testing.T) {
	office, _ := domain.ResolveOffice("Crystal River")
	ladder := office.AppointmentTypes.Medical
	if ladder.NewAdult == ladder.EstablishedAdult {
		t.Fatal("fixture office must distinguish new and established adult medical types")
	}
	for _, c := range []struct {
		name      string
		completed bool
		want      int
	}{
		{"never seen", false, ladder.NewAdult},
		{"seen", true, ladder.EstablishedAdult},
	} {
		t.Run(c.name, func(t *testing.T) {
			records := patientStatusRecords()
			records.CompletedVisits["12345"] = c.completed
			receipt, err := bookMedicalAtCrystalRiver(t, records)
			if err != nil || len(records.Bookings) != 1 || receipt.AppointmentTypeID != c.want {
				t.Fatalf("receipt=%+v err=%v writes=%d, want type %d", receipt, err, len(records.Bookings), c.want)
			}
			if records.CompletedVisitCalls != 1 {
				t.Fatalf("completed-visit reads = %d, want 1", records.CompletedVisitCalls)
			}
		})
	}
}

func TestBookingFailsVisiblyWhenVisitHistoryCannotBeRead(t *testing.T) {
	records := patientStatusRecords()
	records.CompletedVisitErrors["12345"] = advancedmd.NewError(safeerrors.CategoryUnavailable)
	_, err := bookMedicalAtCrystalRiver(t, records)
	if scheduling.CategoryOf(err) != scheduling.CategoryWriteFailed || !strings.Contains(err.Error(), "Unable to verify patient before booking") {
		t.Fatalf("error=%v category=%s, want a visible write failure", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("booked without visit history: %+v", records.Bookings)
	}
}
