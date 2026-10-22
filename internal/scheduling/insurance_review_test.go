package scheduling_test

import (
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
	"context"
	"testing"
	"time"
)

func TestBookingUsesDirectoryRoundTripAndMedicalCategory(t *testing.T) {
	records := recordsWithSetup(testColumn("1268", "620", "1480", "09:00", "09:15", 15))
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "AETNA", CarrierID: "car40887"}
	now := mutationTestNow()
	token, err := scheduling.SignSlotToken("test-booking-secret", scheduling.SlotPolicy{
		OfficeID: "hollywood", Routing: "bach_only", ColumnID: 1268, ProfileID: 620,
		StartDatetime: "2026-06-03T09:00", Duration: 15, DOB: "01/15/1980",
		AppointmentTypeIDs: []int{1006, 1007}, SameStartCapacity: 2,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("SignSlotToken error = %v", err)
	}
	svc := scheduling.New(records, "test-booking-secret", func() time.Time { return now })
	_, err = svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: "Hollywood", VisitCategory: "medical", AppointmentTypeID: 1007, BookingToken: token})
	if err != nil || len(records.Bookings) != 1 {
		t.Fatalf("confirmed product failed after AMD-shaped read: err=%v writes=%d", err, len(records.Bookings))
	}
}
