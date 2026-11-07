package scheduling_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

type overlapRecords struct {
	*advancedmdtest.Adapter
	visitsStarted chan struct{}
	dayStarted    chan struct{}
	dayOnce       sync.Once
}

func (r *overlapRecords) HasCompletedVisit(ctx context.Context, patientID string) (bool, error) {
	close(r.visitsStarted)
	return r.Adapter.HasCompletedVisit(ctx, patientID)
}

func (r *overlapRecords) ReadColumnDay(ctx context.Context, columnID int, date string) (domain.ColumnSchedule, error) {
	r.dayOnce.Do(func() { close(r.dayStarted) })
	return r.Adapter.ReadColumnDay(ctx, columnID, date)
}

func (r *overlapRecords) GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error) {
	for _, started := range []chan struct{}{r.visitsStarted, r.dayStarted} {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			return domain.PatientDemographics{}, errors.New("pre-write reads ran one after another")
		}
	}
	return r.Adapter.GetPatientDemographics(ctx, patientID)
}

func TestBookRunsItsPreWriteReadsTogether(t *testing.T) {
	records := patientStatusRecords()
	records.BookAppointmentID = 777
	now := mutationTestNow()
	available, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).List(context.Background(), plannedSearch(scheduling.ListCommand{
		PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical",
	}))
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("List() = %+v, %v", available, err)
	}

	overlap := &overlapRecords{Adapter: records, visitsStarted: make(chan struct{}), dayStarted: make(chan struct{})}
	receipt, err := scheduling.New(overlap, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), scheduling.BookCommand{
		PatientID: "12345", DOB: "01/15/1980", BookingToken: available.Slots[0].BookingToken,
		VisitCategory: "medical", AppointmentReason: "Blurry vision", ReferringDoctor: "none",
	})
	if err != nil || receipt.Status != "booked" {
		t.Fatalf("Book() = %+v, %v", receipt, err)
	}
	if len(records.Bookings) != 1 {
		t.Fatalf("bookings = %d, want 1", len(records.Bookings))
	}
}

type panickingVisitRecords struct {
	*advancedmdtest.Adapter
}

func (panickingVisitRecords) HasCompletedVisit(context.Context, string) (bool, error) {
	panic("visit read failed")
}

func TestBookRaisesAPreWriteReadPanicOnTheCallingGoroutine(t *testing.T) {
	records := patientStatusRecords()
	now := mutationTestNow()
	available, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).List(context.Background(), plannedSearch(scheduling.ListCommand{
		PatientID: "12345", Office: "Crystal River", StartDate: "2026-06-03", DOB: "01/15/1980", VisitType: "medical",
	}))
	if err != nil || len(available.Slots) == 0 {
		t.Fatalf("List() = %+v, %v", available, err)
	}

	defer func() {
		if recovered := recover(); recovered != "visit read failed" || len(records.Bookings) != 0 {
			t.Fatalf("recovered = %v, bookings = %d; want the read's panic on this goroutine and no booking", recovered, len(records.Bookings))
		}
	}()
	scheduling.New(panickingVisitRecords{records}, "test-booking-secret", func() time.Time { return now }).Book(context.Background(), scheduling.BookCommand{
		PatientID: "12345", DOB: "01/15/1980", BookingToken: available.Slots[0].BookingToken,
		VisitCategory: "medical", AppointmentReason: "Blurry vision", ReferringDoctor: "none",
	})
}
