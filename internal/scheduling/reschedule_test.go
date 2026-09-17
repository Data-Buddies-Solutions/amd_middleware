package scheduling_test

import (
	"context"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/scheduling"
)

func rescheduleFixture(t *testing.T) (*advancedmdtest.Adapter, scheduling.BookCommand, domain.PatientAppointment) {
	t.Helper()
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentID = 98765
	old := matchingAppointmentAt(54321, time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC))
	records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{old}, true)
	command := signedBookCommand(t, now)
	command.VisitCategory = "medical"
	var err error
	command.RescheduleToken, err = scheduling.NewAppointmentTokens("test-booking-secret", func() time.Time { return now }).IssueRescheduleToken("12345", old)
	if err != nil {
		t.Fatal(err)
	}
	return records, command, old
}

func TestRescheduleOutcomes(t *testing.T) {
	for _, scenario := range []string{"success", "rejected booking", "ambiguous booking success", "ambiguous booking absent", "ambiguous booking unknown", "rejected cancellation", "ambiguous cancellation success", "ambiguous cancellation unknown"} {
		t.Run(scenario, func(t *testing.T) {
			records, command, old := rescheduleFixture(t)
			expected, bookings, cancellations := "completed", 1, 1
			switch scenario {
			case "rejected booking":
				records.BookAppointmentErr = advancedmd.NewError(safeerrors.CategoryRejected)
				expected = "failed"
				cancellations = 0
			case "ambiguous booking success":
				records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{old, matchingAppointment(98765)}, true)
			case "ambiguous booking absent":
				records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				expected = "failed"
				cancellations = 0
			case "ambiguous booking unknown":
				// Use an adapter wrapper below to fail only the reconciliation read.
				records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				expected = "uncertain"
				cancellations = 0
			case "rejected cancellation":
				records.CancelAppointmentErr = advancedmd.NewError(safeerrors.CategoryRejected)
				expected = "partial"
			case "ambiguous cancellation success":
				records.CancelAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				records.AppointmentStateResults[old.ID] = advancedmdtest.AppointmentStateResult{State: advancedmd.AppointmentState{Complete: true}}
			case "ambiguous cancellation unknown":
				records.AppointmentStateResults[old.ID] = advancedmdtest.AppointmentStateResult{State: advancedmd.AppointmentState{Complete: false}}
				records.CancelAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
				expected = "partial"

			}
			var provider advancedmd.SchedulingRecords = records
			if scenario == "ambiguous booking unknown" {
				provider = &failReconciliation{Adapter: records}
			}
			result, err := scheduling.New(provider, "test-booking-secret", mutationTestNow).Reschedule(context.Background(), command)
			if err != nil || result.Status != expected {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if expected == "completed" || expected == "partial" {
				if result.Booking == nil || result.Booking.AppointmentID != 98765 || result.Booking.VisitType != "medical" || result.Booking.OfficeID != "spring_hill" || result.Booking.CancellationToken == "" || result.Booking.RescheduleToken == "" {
					t.Fatalf("missing replacement metadata: %+v", result.Booking)
				}
			}

			if len(records.Bookings) != bookings || len(records.Cancellations) != cancellations {
				t.Fatalf("writes=%d/%d want=%d/%d", len(records.Bookings), len(records.Cancellations), bookings, cancellations)
			}
			if cancellations > 0 && records.Cancellations[0].AppointmentID != old.ID {
				t.Fatal("wrong original cancelled")
			}
		})
	}
}

type failReconciliation struct {
	*advancedmdtest.Adapter
	reads int
}

func (a *failReconciliation) ReadPatientAppointmentsForMonth(ctx context.Context, q advancedmd.AppointmentMonthQuery) (advancedmd.AppointmentRead, error) {
	a.reads++
	if a.reads > 1 {
		return advancedmd.AppointmentRead{}, advancedmd.NewError(safeerrors.CategoryUnavailable)
	}
	return a.Adapter.ReadPatientAppointmentsForMonth(ctx, q)
}

func TestRescheduleCanRetryAfterConfirmedNoWriteFailure(t *testing.T) {
	for _, scenario := range []string{"appointment read", "demographics", "slot"} {
		t.Run(scenario, func(t *testing.T) {
			records, command, _ := rescheduleFixture(t)
			original := records.AppointmentResults["12345"]
			switch scenario {
			case "appointment read":
				records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{Err: advancedmd.NewError(safeerrors.CategoryUnavailable)}
			case "demographics":
				records.DemographicErrors["12345"] = advancedmd.NewError(safeerrors.CategoryUnavailable)
			case "slot":
				records.ScheduleReadErrors["2026-06-03"] = advancedmd.NewError(safeerrors.CategoryUnavailable)
			}
			service := scheduling.New(records, "test-booking-secret", mutationTestNow)
			result, err := service.Reschedule(context.Background(), command)
			if err == nil && result.Status != "failed" {
				t.Fatalf("initial=%+v %v", result, err)
			}
			if len(records.Bookings) != 0 || len(records.Cancellations) != 0 {
				t.Fatal("unexpected write during failure")
			}
			records.AppointmentResults["12345"] = original
			delete(records.DemographicErrors, "12345")
			delete(records.ScheduleReadErrors, "2026-06-03")
			result, err = service.Reschedule(context.Background(), command)
			if err != nil || result.Status != "completed" || len(records.Bookings) != 1 || len(records.Cancellations) != 1 {
				t.Fatalf("retry=%+v %v", result, err)
			}
		})
	}
}

func TestRescheduleInvalidAuthorityCannotWrite(t *testing.T) {
	for _, kind := range []string{"patient", "token", "visit", "booking token", "missing original"} {
		t.Run(kind, func(t *testing.T) {
			records, command, _ := rescheduleFixture(t)
			switch kind {
			case "patient":
				command.PatientID = "999"
			case "token":
				command.RescheduleToken = "invalid"
			case "visit":
				command.VisitCategory = "routine_vision"
			case "booking token":
				command.BookingToken = ""
			case "missing original":
				records.AppointmentResults["12345"] = appointmentResult(nil, true)
			}
			_, err := scheduling.New(records, "test-booking-secret", mutationTestNow).Reschedule(context.Background(), command)
			if err == nil || len(records.Bookings) > 0 || len(records.Cancellations) > 0 {
				t.Fatal("unsafe write")
			}
		})
	}
}

func TestAppointmentClassificationAndBackendVisitPolicy(t *testing.T) {
	for _, tc := range []struct {
		id    int
		visit string
	}{{1004, "medical"}, {1008, "medical"}, {6167, "medical"}, {6169, "medical"}, {1010, "routine_vision"}, {4245, "routine_vision"}, {9999, ""}, {0, ""}} {
		if got := domain.AppointmentVisitType(tc.id); got != tc.visit {
			t.Fatalf("type %d = %q", tc.id, got)
		}
	}
	for _, tc := range []struct{ office, visit, routing string }{{"Crystal River", "routine_vision", "optical_only"}, {"North Miami Beach Optical", "medical", "optical_only"}, {"Spring Hill", "medical", "optical_only"}} {
		records := bookingRecords()
		result, err := scheduling.New(records, "test-booking-secret", mutationTestNow).List(context.Background(), scheduling.ListCommand{Office: tc.office, VisitType: tc.visit, Routing: tc.routing, DOB: "01/15/1980"})
		if err != nil || result.Outcome != domain.AvailabilityOutcomeNoEligibleProviders || len(result.Slots) != 0 {
			t.Fatalf("%+v: %+v %v", tc, result, err)
		}
		if records.SchedulerSetupCalls != 0 {
			t.Fatal("unsupported visit should not read provider schedule")
		}
	}
}

// A changed original during the booking write cannot be cancelled using stale
// authorization. The replacement remains an explicit partial result.
type changedOriginal struct{ *advancedmdtest.Adapter }

func (a *changedOriginal) BookAppointment(ctx context.Context, b advancedmd.Booking) (int, error) {
	id, err := a.Adapter.BookAppointment(ctx, b)
	result := a.AppointmentResults["12345"]
	result.Read.Appointments[0].Start = result.Read.Appointments[0].Start.Add(time.Hour)
	a.AppointmentResults["12345"] = result
	return id, err
}
func TestRescheduleDoesNotCancelOriginalChangedDuringBooking(t *testing.T) {
	records, command, _ := rescheduleFixture(t)
	result, err := scheduling.New(&changedOriginal{records}, "test-booking-secret", mutationTestNow).Reschedule(context.Background(), command)
	if err != nil || result.Status != "partial" || result.Booking == nil || len(records.Bookings) != 1 || len(records.Cancellations) != 0 {
		t.Fatalf("result=%+v err=%v cancellations=%d", result, err, len(records.Cancellations))
	}
}
