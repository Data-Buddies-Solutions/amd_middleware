package scheduling_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

func TestBookingRequestDoesNotDecodeServerOwnedFields(t *testing.T) {
	for _, field := range []string{`"insurancePlan":"ignored"`, `"columnId":999`, `"profileId":999`, `"startDatetime":"2026-06-03T10:00"`, `"duration":60`, `"routing":"optical_only"`} {
		t.Run(field, func(t *testing.T) {
			decoder := json.NewDecoder(strings.NewReader("{" + field + "}"))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&scheduling.BookCommand{}); err == nil {
				t.Fatalf("obsolete request field accepted: %s", field)
			}
		})
	}
}

func TestBookingAndRescheduleUseSignedSlotFields(t *testing.T) {
	for _, reschedule := range []bool{false, true} {
		t.Run(fmt.Sprintf("reschedule=%t", reschedule), func(t *testing.T) {
			records, command, _ := rescheduleFixture(t)
			if !reschedule {
				command.RescheduleToken = ""
			}
			svc := scheduling.New(records, "test-booking-secret", mutationTestNow)
			var receipt scheduling.BookReceipt
			if reschedule {
				result, err := svc.Reschedule(context.Background(), command)
				if err != nil || result.Status != "completed" || result.Booking == nil {
					t.Fatalf("reschedule=%+v err=%v", result, err)
				}
				receipt = *result.Booking
			} else {
				var err error
				receipt, err = svc.Book(context.Background(), command)
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(records.Bookings) != 1 {
				t.Fatalf("writes=%d", len(records.Bookings))
			}
			write := records.Bookings[0]
			if write.ColumnID != 1513 || write.ProfileID != 620 || write.Start.Format(domain.SlotDateTimeLayout) != "2026-06-03T09:00" || write.Duration != 15 || write.AppointmentTypeID != 1007 {
				t.Fatalf("signed slot changed: %+v", write)
			}
			if receipt.ProfileID != "620" || receipt.StartDatetime != "2026-06-03T09:00" || receipt.Duration != 15 || receipt.ProviderName != "Dr. Austin Bach" {
				t.Fatalf("receipt changed: %+v", receipt)
			}
		})
	}
}
