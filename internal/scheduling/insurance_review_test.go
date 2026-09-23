package scheduling_test

import (
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
	"context"
	"testing"
	"time"
)

func TestBookingUsesDirectoryRoundTripAndNormalizedMedicalCategory(t *testing.T) {
	for _, category := range []string{"medical", "medical visit", "follow up"} {
		t.Run(category, func(t *testing.T) {
			records := recordsWithSetup(testColumn("1268", "620", "1480", "09:00", "09:15", 15))
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: "AETNA", CarrierID: "car40887"}
			records.ScheduleReads["2026-06-03"] = completeRead("1268", nil, nil)
			now := mutationTestNow()
			svc := scheduling.NewWithConfig(records, "test-booking-secret", func() time.Time { return now }, scheduling.Config{AllowRawBooking: true})
			_, err := svc.Book(context.Background(), scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: "Hollywood", InsurancePlan: "Aetna Commercial", VisitCategory: category, ColumnID: 1268, ProfileID: 620, StartDatetime: "2026-06-03T09:00", Duration: 15, AppointmentTypeID: 1007, Routing: "bach_only"})
			if err != nil || len(records.Bookings) != 1 {
				t.Fatalf("confirmed product failed after AMD-shaped read: err=%v writes=%d", err, len(records.Bookings))
			}
		})
	}
}
