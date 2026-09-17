package scheduling_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/scheduling"
)

type memoryReschedules struct {
	mu       sync.Mutex
	records  map[string]scheduling.RescheduleRecord
	failSave bool
}

func (m *memoryReschedules) Claim(_ context.Context, key string, record scheduling.RescheduleRecord) (scheduling.RescheduleRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[string]scheduling.RescheduleRecord{}
	}
	if old, ok := m.records[key]; ok {
		return old, false, nil
	}
	m.records[key] = record
	return record, true, nil
}
func (m *memoryReschedules) Save(_ context.Context, key string, record scheduling.RescheduleRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSave {
		return errors.New("storage unavailable")
	}
	m.records[key] = record
	return nil
}

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

func TestRescheduleOutcomesAndRetryAcrossInstances(t *testing.T) {
	for _, scenario := range []string{"success", "rejected booking", "ambiguous booking success", "ambiguous booking absent", "ambiguous booking unknown", "rejected cancellation", "ambiguous cancellation success", "ambiguous cancellation unknown", "receipt failure", "missing original"} {
		t.Run(scenario, func(t *testing.T) {
			records, command, old := rescheduleFixture(t)
			store := &memoryReschedules{}
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
			case "receipt failure":
				store.failSave = true
				expected = "partial"
				cancellations = 0
			case "missing original":
				records.AppointmentResults["12345"] = appointmentResult(nil, true)
				expected = "failed"
				bookings = 0
				cancellations = 0
			}
			var provider advancedmd.SchedulingRecords = records
			if scenario == "ambiguous booking unknown" {
				provider = &failReconciliation{Adapter: records}
			}
			newService := func() scheduling.Scheduling {
				return scheduling.NewWithConfig(provider, "test-booking-secret", mutationTestNow, scheduling.Config{Reschedules: store})
			}
			result, err := newService().Reschedule(context.Background(), command)
			if err != nil || result.Status != expected {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if expected == "completed" || expected == "partial" {
				if result.Booking == nil || result.Booking.AppointmentID != 98765 || result.Booking.VisitType != "medical" || result.Booking.OfficeID != "spring_hill" || result.Booking.CancellationToken == "" || result.Booking.RescheduleToken == "" {
					t.Fatalf("missing replacement metadata: %+v", result.Booking)
				}
			}
			for i := 0; i < 2; i++ {
				replay, err := newService().Reschedule(context.Background(), command)
				if err != nil {
					t.Fatal(err)
				}
				want := expected
				if scenario == "receipt failure" {
					want = "uncertain"
				}
				if replay.Status != want {
					t.Fatalf("replay=%+v", replay)
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

func TestRescheduleConcurrentClaimAndChangedSelection(t *testing.T) {
	records, command, _ := rescheduleFixture(t)
	store := &memoryReschedules{}
	start, release := make(chan struct{}, 1), make(chan struct{})
	records.DemographicsStarted = start
	records.DemographicsRelease = release
	service := scheduling.NewWithConfig(records, "test-booking-secret", mutationTestNow, scheduling.Config{Reschedules: store})
	done := make(chan error, 1)
	go func() { _, err := service.Reschedule(context.Background(), command); done <- err }()
	<-start
	replay, err := service.Reschedule(context.Background(), command)
	if err != nil || replay.Status != "uncertain" {
		t.Fatalf("concurrent=%+v %v", replay, err)
	}
	changed := signedBookCommandAt(t, mutationTestNow(), time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC))
	changed.RescheduleToken = command.RescheduleToken
	conflict, err := service.Reschedule(context.Background(), changed)
	if err != nil || conflict.Outcome != "reschedule_conflict" {
		t.Fatalf("conflict=%+v %v", conflict, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(records.Bookings) != 1 || len(records.Cancellations) != 1 {
		t.Fatal("duplicate writes")
	}
}

func TestRescheduleInvalidAuthorityAndMissingStoreCannotWrite(t *testing.T) {
	for _, kind := range []string{"patient", "token", "visit", "store"} {
		t.Run(kind, func(t *testing.T) {
			records, command, _ := rescheduleFixture(t)
			config := scheduling.Config{Reschedules: &memoryReschedules{}}
			switch kind {
			case "patient":
				command.PatientID = "999"
			case "token":
				command.RescheduleToken = "invalid"
			case "visit":
				command.VisitCategory = "routine_vision"
			case "store":
				config.Reschedules = nil
			}
			_, err := scheduling.NewWithConfig(records, "test-booking-secret", mutationTestNow, config).Reschedule(context.Background(), command)
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
	result, err := scheduling.NewWithConfig(&changedOriginal{records}, "test-booking-secret", mutationTestNow, scheduling.Config{Reschedules: &memoryReschedules{}}).Reschedule(context.Background(), command)
	if err != nil || result.Status != "partial" || result.Booking == nil || len(records.Bookings) != 1 || len(records.Cancellations) != 0 {
		t.Fatalf("result=%+v err=%v cancellations=%d", result, err, len(records.Cancellations))
	}
}
