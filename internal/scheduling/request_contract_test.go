package scheduling_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/scheduling"
)

func TestBookingCommentIncludesReferringDoctorContact(t *testing.T) {
	for _, scenario := range []struct {
		name, phone, address, expected string
	}{
		{"with contact", " 352-555-0100 ", "12 Main St", "Appointment reason: Eye follow-up\nReferring doctor: Dr. Lee\nReferring doctor phone: 352-555-0100\nReferring doctor address: 12 Main St\n- AI"},
		{"name only", "", "", "Appointment reason: Eye follow-up\nReferring doctor: Dr. Lee\n- AI"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			records, command, _ := rescheduleFixture(t)
			command.RescheduleToken = ""
			command.AppointmentReason = "Eye follow-up"
			command.ReferringDoctor = "Dr. Lee"
			command.ReferringDoctorPhone = scenario.phone
			command.ReferringDoctorAddress = scenario.address
			if _, err := scheduling.New(records, "test-booking-secret", mutationTestNow).Book(context.Background(), command); err != nil {
				t.Fatal(err)
			}
			if len(records.Bookings) != 1 || records.Bookings[0].Comments != scenario.expected {
				t.Fatalf("bookings=%+v", records.Bookings)
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
			if write.ColumnID != 1513 || write.ProfileID != 620 || write.Start.Format(domain.SlotDateTimeLayout) != "2026-06-03T09:00" || write.Duration != 15 || write.ProviderAppointmentTypeID != 1007 {
				t.Fatalf("signed slot changed: %+v", write)
			}
			if receipt.ProfileID != "620" || receipt.StartDatetime != "2026-06-03T09:00" || receipt.Duration != 15 || receipt.ProviderName != "Dr. Austin Bach" {
				t.Fatalf("receipt changed: %+v", receipt)
			}
		})
	}
}

func TestBookRejectsUnknownIntentBeforeProviderCall(t *testing.T) {
	values := []string{"", "unknown", "Medical", " medical ", "medical visit", "follow up", "optical", "routine vision", "created", "matched", "new patient", "Established"}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			command := signedBookCommand(t, mutationTestNow())
			command.AppointmentTypeID = 0
			command.VisitCategory = value
			_, err := scheduling.New(nil, "test-booking-secret", mutationTestNow).Book(context.Background(), command)
			if scheduling.CategoryOf(err) != scheduling.CategoryValidation || !strings.Contains(err.Error(), "visitCategory") {
				t.Fatalf("error=%v category=%s, want validation naming visitCategory", err, scheduling.CategoryOf(err))
			}
		})
	}
}

func TestBookAllowsOmittedIntentWithExplicitOrPreservedType(t *testing.T) {
	for _, mode := range []string{"explicit", "preserved_on_book", "reschedule"} {
		t.Run(mode, func(t *testing.T) {
			records, command, _ := rescheduleFixture(t)
			command.VisitCategory = ""
			if mode == "explicit" {
				command.RescheduleToken = ""
			} else {
				command.AppointmentTypeID = 0
			}
			svc := scheduling.New(records, "test-booking-secret", mutationTestNow)
			if mode == "reschedule" {
				result, err := svc.Reschedule(context.Background(), command)
				if err != nil || result.Status != "completed" {
					t.Fatalf("reschedule=%+v error=%v", result, err)
				}
			} else {
				_, err := svc.Book(context.Background(), command)
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(records.Bookings) != 1 || records.Bookings[0].ProviderAppointmentTypeID != 1007 {
				t.Fatalf("bookings=%+v", records.Bookings)
			}
			if records.CompletedVisitCalls != 0 {
				t.Fatalf("a fixed appointment type read visit history %d times", records.CompletedVisitCalls)
			}
		})
	}
}

func TestBookRejectsUnknownIntentEvenWithExplicitOrPreservedType(t *testing.T) {
	for _, mode := range []string{"explicit", "preserved_on_book", "reschedule"} {
		t.Run(mode, func(t *testing.T) {
			_, command, _ := rescheduleFixture(t)
			if mode == "explicit" {
				command.RescheduleToken = ""
			} else {
				command.AppointmentTypeID = 0
			}
			command.VisitCategory = "unknown"
			svc := scheduling.New(nil, "test-booking-secret", mutationTestNow)
			var err error
			if mode == "reschedule" {
				_, err = svc.Reschedule(context.Background(), command)
			} else {
				_, err = svc.Book(context.Background(), command)
			}
			if scheduling.CategoryOf(err) != scheduling.CategoryValidation || !strings.Contains(err.Error(), "visitCategory") {
				t.Fatalf("error=%v, want validation naming visitCategory", err)
			}
		})
	}
}
