package scheduling_test

import (
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
	"context"
	"strings"
	"testing"
	"time"
)

func TestSharedCarrierCannotBypassChartAuthorizationAtBooking(t *testing.T) {
	for _, tc := range []struct{ chart, requested, carrier string }{
		{"Cigna HMO", "Cigna PPO", "car301345"},
		{"United Healthcare NHP HMO Only", "United Healthcare NHP HMO Access", "car40923"},
	} {
		t.Run(tc.chart, func(t *testing.T) {
			records := recordsWithSetup(testColumn("1268", "620", "1480", "09:00", "09:15", 15))
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: tc.chart, CarrierID: tc.carrier}
			records.ScheduleReads["2026-06-03"] = completeRead("1268", nil, nil)
			now := mutationTestNow()
			svc := scheduling.NewWithConfig(records, "test-booking-secret", func() time.Time { return now }, scheduling.Config{AllowRawBooking: true})
			command := scheduling.BookCommand{PatientID: "12345", DOB: "01/15/1980", Office: "Hollywood", InsurancePlan: tc.requested, ColumnID: 1268, ProfileID: 620, StartDatetime: "2026-06-03T09:00", Duration: 15, AppointmentTypeID: 1007, Routing: "bach_only"}
			_, err := svc.Book(context.Background(), command)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "insurance") || len(records.Bookings) != 0 {
				t.Fatalf("chart product restrictions bypassed: err=%v writes=%d", err, len(records.Bookings))
			}
			// The same fixture and requested product are bookable when the chart agrees.
			records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980", CarrierName: tc.requested, CarrierID: tc.carrier}
			if _, err := svc.Book(context.Background(), command); err != nil {
				t.Fatalf("control with matching product failed: %v", err)
			}
			if len(records.Bookings) != 1 {
				t.Fatalf("control writes=%d", len(records.Bookings))
			}
		})
	}
}
