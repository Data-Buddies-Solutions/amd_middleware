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

func TestBookReturnsReceiptAfterRevalidatingSignedSlot(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentID = 98765

	receipt, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), signedBookCommand(t, now))
	if err != nil {
		t.Fatalf("Book error = %v", err)
	}
	if receipt.Status != "booked" ||
		receipt.AppointmentID != 98765 ||
		receipt.PatientID != "12345" ||
		receipt.PatientName != "Jane Doe" ||
		receipt.ProviderName != "Dr. Austin Bach" ||
		receipt.LocationName != "Spring Hill" ||
		receipt.StartDatetime != "2026-06-03T09:00" ||
		receipt.Duration != 15 ||
		receipt.AppointmentTypeID != 1007 ||
		receipt.AppointmentTypeName != "Established Adult Medical (Follow Up)" ||
		receipt.Message != "Appointment booked successfully" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(records.Bookings) != 1 || records.Bookings[0].Force {
		t.Fatalf("provider bookings = %#v", records.Bookings)
	}
}

func TestBookRejectsInvalidSignedSlotBeforeWrite(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	command := signedBookCommand(t, now)
	command.BookingToken += "tampered"

	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), command)
	if scheduling.CategoryOf(err) != scheduling.CategoryInvalidBookingToken {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("provider bookings = %d, want none", len(records.Bookings))
	}
}

func TestBookIncompleteRevalidationCanRetryWithoutClaimingSlotConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		columns map[string]domain.ColumnSchedule
	}{
		{"appointments unavailable", map[string]domain.ColumnSchedule{
			"1513": {BlockHoldsComplete: true},
		}},
		{"block holds unavailable", map[string]domain.ColumnSchedule{
			"1513": {AppointmentsComplete: true},
		}},
		{"provider column missing", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := mutationTestNow()
			records := bookingRecords()
			records.BookAppointmentID = 98765
			records.ScheduleReads["2026-06-03"] = domain.ScheduleReadResult{Columns: tc.columns}
			command := signedBookCommand(t, now)
			scheduler := scheduling.New(records, "test-booking-secret", func() time.Time { return now })

			_, err := scheduler.Book(context.Background(), command)
			if scheduling.CategoryOf(err) != scheduling.CategoryWriteFailed ||
				scheduling.ProviderFailureOf(err) != safeerrors.CategoryInvalidResponse {
				t.Fatalf("Book error = %v, category = %q, provider failure = %q; want retryable provider-read failure", err, scheduling.CategoryOf(err), scheduling.ProviderFailureOf(err))
			}
			if err.Error() != "Unable to verify the selected time because appointment data is incomplete. Please try once more or contact the office." {
				t.Fatalf("Book message = %q, want an explanation of failed verification", err.Error())
			}
			if len(records.Bookings) != 0 {
				t.Fatalf("provider bookings = %d, want none before complete verification", len(records.Bookings))
			}

			records.ScheduleReads["2026-06-03"] = completeRead("1513", nil, nil)
			receipt, err := scheduler.Book(context.Background(), command)
			if err != nil || receipt.Status != "booked" || len(records.Bookings) != 1 {
				t.Fatalf("retry receipt = %#v, error = %v, provider bookings = %d; want one successful booking", receipt, err, len(records.Bookings))
			}
		})
	}
}

func TestBookPreservesAnyAppointmentTypeFromSignedRescheduleToken(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentID = 98765
	command := signedBookCommand(t, now)
	command.AppointmentTypeID = 1007
	rescheduleToken, err := scheduling.NewAppointmentTokens(
		"test-booking-secret",
		func() time.Time { return now },
	).IssueRescheduleToken("12345", domain.PatientAppointment{
		ID:                54321,
		Start:             time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		AppointmentTypeID: 9999,
		OfficeID:          "spring_hill",
	})
	if err != nil {
		t.Fatalf("IssueCancellationToken error = %v", err)
	}
	command.RescheduleToken = rescheduleToken

	receipt, err := scheduling.New(
		records,
		"test-booking-secret",
		func() time.Time { return now },
	).Book(context.Background(), command)
	if err != nil {
		t.Fatalf("Book error = %v", err)
	}
	if receipt.AppointmentTypeID != 9999 {
		t.Fatalf("receipt appointment type = %d, want 9999", receipt.AppointmentTypeID)
	}
	if receipt.RescheduleToken == "" {
		t.Fatal("receipt reschedule token is empty")
	}
	if len(records.Bookings) != 1 ||
		records.Bookings[0].AppointmentTypeID != 9999 ||
		records.Bookings[0].ProviderAppointmentTypeID != 9999 ||
		records.Bookings[0].AppointmentColor != domain.PreservedAppointmentTypeFallbackColor {
		t.Fatalf("provider bookings = %#v", records.Bookings)
	}

	corrected := signedBookCommand(t, now)
	corrected.RescheduleToken = receipt.RescheduleToken
	records.ScheduleReads["2026-06-03"] = completeRead("1513", nil, nil)
	if _, err := scheduling.New(
		records,
		"test-booking-secret",
		func() time.Time { return now },
	).Book(context.Background(), corrected); err != nil {
		t.Fatalf("corrected Book error = %v", err)
	}
	if len(records.Bookings) != 2 || records.Bookings[1].AppointmentTypeID != 9999 {
		t.Fatalf("corrected provider bookings = %#v", records.Bookings)
	}
}

func TestBookRejectsCancellationTokenAsRescheduleAuthorization(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	command := signedBookCommand(t, now)
	token, err := scheduling.NewAppointmentTokens(
		"test-booking-secret",
		func() time.Time { return now },
	).IssueCancellationToken("12345", domain.PatientAppointment{
		ID:                54321,
		Start:             time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
		AppointmentTypeID: 9999,
		OfficeID:          "spring_hill",
	})
	if err != nil {
		t.Fatalf("IssueCancellationToken error = %v", err)
	}
	command.RescheduleToken = token

	_, err = scheduling.New(
		records,
		"test-booking-secret",
		func() time.Time { return now },
	).Book(context.Background(), command)
	if scheduling.CategoryOf(err) != scheduling.CategoryInvalidRescheduleToken {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("provider bookings = %d, want none", len(records.Bookings))
	}
}

func TestBookRejectsInvalidRescheduleTokenBeforeWrite(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	command := signedBookCommand(t, now)
	command.RescheduleToken = "invalid-token"

	_, err := scheduling.New(
		records,
		"test-booking-secret",
		func() time.Time { return now },
	).Book(context.Background(), command)
	if scheduling.CategoryOf(err) != scheduling.CategoryInvalidRescheduleToken {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("provider bookings = %d, want none", len(records.Bookings))
	}
}

func TestBookStillRejectsUnrecognizedTypeWithoutRescheduleAuthorization(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	command := signedBookCommand(t, now)
	command.AppointmentTypeID = 9999

	_, err := scheduling.New(
		records,
		"test-booking-secret",
		func() time.Time { return now },
	).Book(context.Background(), command)
	if scheduling.CategoryOf(err) != scheduling.CategoryValidation {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("provider bookings = %d, want none", len(records.Bookings))
	}
}

func TestBookRevalidatesPatientOfficeTypeProviderCapacityAndForce(t *testing.T) {
	now := mutationTestNow()
	tests := []struct {
		name     string
		mutate   func(*advancedmdtest.Adapter, *scheduling.BookCommand)
		category scheduling.Category
	}{
		{
			name: "patient context",
			mutate: func(records *advancedmdtest.Adapter, _ *scheduling.BookCommand) {
				records.Demographics["12345"] = domain.PatientDemographics{DOB: "02/20/1990"}
			},
			category: scheduling.CategoryPatientContextMismatch,
		},
		{
			name: "office",
			mutate: func(_ *advancedmdtest.Adapter, command *scheduling.BookCommand) {
				command.Office = "Hollywood"
			},
			category: scheduling.CategoryInvalidBookingToken,
		},
		{
			name: "appointment type",
			mutate: func(_ *advancedmdtest.Adapter, command *scheduling.BookCommand) {
				command.AppointmentTypeID = 1006
			},
			category: scheduling.CategoryInvalidBookingToken,
		},
		{
			name: "provider eligibility",
			mutate: func(records *advancedmdtest.Adapter, _ *scheduling.BookCommand) {
				records.SchedulerSetup.Columns[0].FacilityID = "999"
			},
			category: scheduling.CategorySlotUnavailable,
		},
		{
			name: "current workday",
			mutate: func(records *advancedmdtest.Adapter, _ *scheduling.BookCommand) {
				records.SchedulerSetup.Columns[0].Workweek = 2
			},
			category: scheduling.CategorySlotUnavailable,
		},
		{
			name: "current work hours",
			mutate: func(records *advancedmdtest.Adapter, _ *scheduling.BookCommand) {
				records.SchedulerSetup.Columns[0].StartTime = "10:00"
			},
			category: scheduling.CategorySlotUnavailable,
		},
		{
			name: "current interval",
			mutate: func(records *advancedmdtest.Adapter, _ *scheduling.BookCommand) {
				records.SchedulerSetup.Columns[0].Interval = 30
			},
			category: scheduling.CategorySlotUnavailable,
		},
		{
			name: "capacity and force decision",
			mutate: func(records *advancedmdtest.Adapter, _ *scheduling.BookCommand) {
				records.ScheduleReads["2026-06-03"] = completeRead("1513", []domain.Appointment{{
					StartDateTime: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
					Duration:      15,
				}}, nil)
			},
			category: scheduling.CategorySlotUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := bookingRecords()
			command := signedBookCommand(t, now)
			tt.mutate(records, &command)

			_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
				Book(context.Background(), command)
			if scheduling.CategoryOf(err) != tt.category {
				t.Fatalf("Book error = %v, category = %q, want %q", err, scheduling.CategoryOf(err), tt.category)
			}
			if len(records.Bookings) != 0 {
				t.Fatalf("provider bookings = %d, want none", len(records.Bookings))
			}
		})
	}
}

func TestBookUsesVerifiedPatientDOBWhenSignedSlotOmittedIt(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "06/01/2020"}
	token, err := scheduling.SignSlotToken("test-booking-secret", scheduling.SlotPolicy{
		OfficeID:           "spring_hill",
		Routing:            string(domain.RoutingBachOnly),
		ColumnID:           1513,
		ProfileID:          620,
		StartDatetime:      "2026-06-03T09:00",
		Duration:           15,
		AppointmentTypeIDs: []int{1007},
		SameStartCapacity:  2,
		Provider:           "Dr. Austin Bach",
		IssuedAt:           now.Unix(),
		ExpiresAt:          now.Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("SignSlotToken error = %v", err)
	}

	_, err = scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), scheduling.BookCommand{
			PatientID:         "12345",
			BookingToken:      token,
			AppointmentTypeID: 1007,
		})
	if scheduling.CategoryOf(err) != scheduling.CategoryValidation {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 0 {
		t.Fatalf("provider bookings = %d, want none", len(records.Bookings))
	}
}

func TestBookReturnsExplicitProviderFailuresWithoutRetry(t *testing.T) {
	now := mutationTestNow()
	tests := []struct {
		name            string
		provider        error
		category        scheduling.Category
		providerFailure safeerrors.Category
	}{
		{
			name:            "conflict",
			provider:        advancedmd.NewError(safeerrors.CategoryConflict),
			category:        scheduling.CategorySlotUnavailable,
			providerFailure: safeerrors.CategoryConflict,
		},
		{
			name:            "rejection",
			provider:        advancedmd.NewError(safeerrors.CategoryRejected),
			category:        scheduling.CategoryProviderRejected,
			providerFailure: safeerrors.CategoryRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := bookingRecords()
			records.BookAppointmentErr = tt.provider

			_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
				Book(context.Background(), signedBookCommand(t, now))
			if scheduling.CategoryOf(err) != tt.category {
				t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
			}
			if got := scheduling.ProviderFailureOf(err); got != tt.providerFailure {
				t.Fatalf("ProviderFailureOf(error) = %q, want %q", got, tt.providerFailure)
			}
			if len(records.Bookings) != 1 {
				t.Fatalf("provider bookings = %d, want exactly one", len(records.Bookings))
			}
		})
	}
}

func TestBookReconcilesAmbiguousWriteToSuccess(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryNetwork)
	records.AppointmentResults["12345"] = appointmentResult(
		[]domain.PatientAppointment{matchingAppointment(98765)},
		true,
	)

	receipt, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), signedBookCommand(t, now))
	if err != nil {
		t.Fatalf("Book error = %v", err)
	}
	if receipt.Status != "booked" || receipt.AppointmentID != 98765 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(records.Bookings) != 1 {
		t.Fatalf("provider bookings = %d, want exactly one", len(records.Bookings))
	}
}

func TestBookReconcilesAmbiguousWriteInIntendedMonth(t *testing.T) {
	now := mutationTestNow()
	start := time.Date(2027, time.January, 3, 9, 0, 0, 0, time.UTC)
	records := bookingRecords()
	records.ScheduleReads[start.Format("2006-01-02")] = completeRead("1513", nil, nil)
	records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryNetwork)
	records.AppointmentResults["12345"] = appointmentResult(
		[]domain.PatientAppointment{matchingAppointmentAt(98766, start)},
		true,
	)

	receipt, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), signedBookCommandAt(t, now, start))
	if err != nil {
		t.Fatalf("Book error = %v", err)
	}
	if receipt.AppointmentID != 98766 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(records.AppointmentMonthQueries) != 1 ||
		!records.AppointmentMonthQueries[0].Month.Equal(start) {
		t.Fatalf("appointment month queries = %#v, want %v", records.AppointmentMonthQueries, start)
	}
}

func TestBookReconciliationRequiresEveryIntendedField(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryNetwork)
	records.AppointmentResults["12345"] = appointmentResult([]domain.PatientAppointment{
		{
			ID:                1,
			Start:             time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC),
			Provider:          "Dr. Austin Bach",
			AppointmentTypeID: 1007,
			OfficeID:          "spring_hill",
		},
		{
			ID:                2,
			Start:             time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
			Provider:          "Dr. Joseph Licht",
			AppointmentTypeID: 1007,
			OfficeID:          "spring_hill",
		},
		{
			ID:                3,
			Start:             time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
			Provider:          "Dr. Austin Bach",
			AppointmentTypeID: 1006,
			OfficeID:          "spring_hill",
		},
		{
			ID:                4,
			Start:             time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
			Provider:          "Dr. Austin Bach",
			AppointmentTypeID: 1007,
			OfficeID:          "crystal_river",
		},
	}, true)

	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), signedBookCommand(t, now))
	if scheduling.CategoryOf(err) != scheduling.CategoryWriteFailed {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 1 {
		t.Fatalf("provider bookings = %d, want exactly one", len(records.Bookings))
	}
}

func TestBookReturnsIndeterminateWhenReconciliationCannotProveOutcome(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
	records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
		Err: advancedmd.NewError(safeerrors.CategoryUnavailable),
	}

	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), signedBookCommand(t, now))
	if scheduling.CategoryOf(err) != scheduling.CategoryIndeterminateWrite {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 1 {
		t.Fatalf("provider bookings = %d, want exactly one", len(records.Bookings))
	}
}

func TestBookReturnsIndeterminateWhenReconciliationReadIsIncomplete(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
	records.AppointmentResults["12345"] = appointmentResult(nil, false)

	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Book(context.Background(), signedBookCommand(t, now))
	if scheduling.CategoryOf(err) != scheduling.CategoryIndeterminateWrite {
		t.Fatalf("Book error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Bookings) != 1 {
		t.Fatalf("provider bookings = %d, want exactly one", len(records.Bookings))
	}
}

func TestBookPreservesConfiguredLegacyRawSlotSuccess(t *testing.T) {
	now := mutationTestNow()
	records := bookingRecords()
	records.BookAppointmentID = 98765

	receipt, err := scheduling.NewWithConfig(records, "test-booking-secret", func() time.Time { return now }, scheduling.Config{
		AllowRawBooking: true,
	}).
		Book(context.Background(), scheduling.BookCommand{
			PatientID:         "12345",
			PatientName:       "DOE,JANE",
			DOB:               "01/15/1980",
			ColumnID:          1513,
			ProfileID:         620,
			StartDatetime:     "2026-06-03T09:00",
			Duration:          15,
			AppointmentTypeID: 1007,
			Routing:           string(domain.RoutingBachOnly),
			Office:            "Spring Hill",
		})
	if err != nil {
		t.Fatalf("Book error = %v", err)
	}
	if receipt.Status != "booked" || receipt.AppointmentID != 98765 {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestCancelReturnsReceiptAfterProvingOwnership(t *testing.T) {
	now := mutationTestNow()
	records := cancellationRecords()

	receipt, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Cancel(context.Background(), cancellationCommand())
	if err != nil {
		t.Fatalf("Cancel error = %v", err)
	}
	if receipt.Status != "cancelled" ||
		receipt.AppointmentID != 33333 ||
		receipt.Message != "Appointment cancelled successfully" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(records.Cancellations) != 1 ||
		records.Cancellations[0].PatientID != "12345" ||
		records.Cancellations[0].AppointmentID != 33333 ||
		records.Cancellations[0].OfficeID != "spring_hill" {
		t.Fatalf("provider cancellations = %#v", records.Cancellations)
	}
}

func TestCancelRejectsOwnershipMismatchBeforeWrite(t *testing.T) {
	now := mutationTestNow()
	records := cancellationRecords()
	command := cancellationCommand()
	command.AppointmentID = 44444

	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Cancel(context.Background(), command)
	if scheduling.CategoryOf(err) != scheduling.CategoryOwnershipMismatch {
		t.Fatalf("Cancel error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Cancellations) != 0 {
		t.Fatalf("provider cancellations = %d, want none", len(records.Cancellations))
	}
}

func TestCancelRequiresCompleteReadToRejectOwnership(t *testing.T) {
	now := mutationTestNow()
	records := advancedmdtest.NewAdapter()
	records.AppointmentResults["12345"] = appointmentResult(nil, false)

	_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
		Cancel(context.Background(), cancellationCommand())
	if scheduling.CategoryOf(err) != scheduling.CategoryWriteFailed {
		t.Fatalf("Cancel error = %v, category = %q", err, scheduling.CategoryOf(err))
	}
	if len(records.Cancellations) != 0 {
		t.Fatalf("provider cancellations = %d, want none", len(records.Cancellations))
	}
}

func TestCancelReturnsExplicitProviderFailuresWithoutRetry(t *testing.T) {
	now := mutationTestNow()
	tests := []struct {
		name            string
		provider        error
		category        scheduling.Category
		providerFailure safeerrors.Category
	}{
		{
			name:            "conflict",
			provider:        advancedmd.NewError(safeerrors.CategoryConflict),
			category:        scheduling.CategoryProviderConflict,
			providerFailure: safeerrors.CategoryConflict,
		},
		{
			name:            "rejection",
			provider:        advancedmd.NewError(safeerrors.CategoryRejected),
			category:        scheduling.CategoryProviderRejected,
			providerFailure: safeerrors.CategoryRejected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := cancellationRecords()
			records.CancelAppointmentErr = tt.provider

			_, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
				Cancel(context.Background(), cancellationCommand())
			if scheduling.CategoryOf(err) != tt.category {
				t.Fatalf("Cancel error = %v, category = %q", err, scheduling.CategoryOf(err))
			}
			if got := scheduling.ProviderFailureOf(err); got != tt.providerFailure {
				t.Fatalf("ProviderFailureOf(error) = %q, want %q", got, tt.providerFailure)
			}
			if len(records.Cancellations) != 1 {
				t.Fatalf("provider cancellations = %d, want exactly one", len(records.Cancellations))
			}
		})
	}
}

func TestCancelReconcilesAmbiguousWriteOutcomes(t *testing.T) {
	now := mutationTestNow()
	tests := []struct {
		name     string
		state    advancedmd.AppointmentState
		stateErr error
		category scheduling.Category
		success  bool
	}{
		{
			name:    "success",
			state:   advancedmd.AppointmentState{Complete: true},
			success: true,
		},
		{
			name:     "failure",
			state:    advancedmd.AppointmentState{Exists: true, Complete: true},
			category: scheduling.CategoryWriteFailed,
		},
		{
			name:     "indeterminate",
			stateErr: advancedmd.NewError(safeerrors.CategoryUnavailable),
			category: scheduling.CategoryIndeterminateWrite,
		},
		{
			name:     "incomplete absence",
			state:    advancedmd.AppointmentState{Complete: false},
			category: scheduling.CategoryIndeterminateWrite,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := cancellationRecords()
			records.AppointmentStateResults[33333] = advancedmdtest.AppointmentStateResult{
				State: tt.state,
				Err:   tt.stateErr,
			}
			records.CancelAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)

			receipt, err := scheduling.New(records, "test-booking-secret", func() time.Time { return now }).
				Cancel(context.Background(), cancellationCommand())
			if tt.success {
				if err != nil || receipt.Status != "cancelled" || receipt.AppointmentID != 33333 {
					t.Fatalf("receipt = %#v, error = %v", receipt, err)
				}
			} else if scheduling.CategoryOf(err) != tt.category {
				t.Fatalf("Cancel error = %v, category = %q, want %q", err, scheduling.CategoryOf(err), tt.category)
			}
			if len(records.Cancellations) != 1 {
				t.Fatalf("provider cancellations = %d, want exactly one", len(records.Cancellations))
			}
			if len(records.AppointmentStateQueries) != 1 ||
				records.AppointmentStateQueries[0].Start != cancellableAppointment().Start {
				t.Fatalf("state queries = %#v, want original appointment month", records.AppointmentStateQueries)
			}
		})
	}
}

func mutationTestNow() time.Time {
	return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
}

func bookingRecords() *advancedmdtest.Adapter {
	records := recordsWithSetup(testColumn("1513", "620", "1568", "09:00", "09:15", 15))
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980"}
	records.ScheduleReads["2026-06-03"] = completeRead("1513", nil, nil)
	return records
}

func signedBookCommand(t *testing.T, now time.Time) scheduling.BookCommand {
	return signedBookCommandAt(t, now, time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC))
}

func signedBookCommandAt(t *testing.T, now, start time.Time) scheduling.BookCommand {
	t.Helper()
	token, err := scheduling.SignSlotToken("test-booking-secret", scheduling.SlotPolicy{
		OfficeID:           "spring_hill",
		Routing:            string(domain.RoutingBachOnly),
		ColumnID:           1513,
		ProfileID:          620,
		StartDatetime:      start.Format("2006-01-02T15:04"),
		Duration:           15,
		DOB:                "01/15/1980",
		AppointmentTypeIDs: []int{1007},
		SameStartBooked:    0,
		SameStartCapacity:  2,
		RequiresForce:      false,
		Provider:           "Dr. Austin Bach",
		IssuedAt:           now.Unix(),
		ExpiresAt:          now.Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("SignSlotToken error = %v", err)
	}
	return scheduling.BookCommand{
		PatientID:         "12345",
		PatientName:       "DOE,JANE",
		DOB:               "01/15/1980",
		BookingToken:      token,
		AppointmentTypeID: 1007,
		Office:            "Spring Hill",
	}
}

func matchingAppointment(id int) domain.PatientAppointment {
	return matchingAppointmentAt(id, time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC))
}

func matchingAppointmentAt(id int, start time.Time) domain.PatientAppointment {
	return domain.PatientAppointment{
		ID:                id,
		Start:             start,
		Provider:          "Dr. Austin Bach",
		AppointmentTypeID: 1007,
		OfficeID:          "spring_hill",
		Office:            "Spring Hill",
	}
}

func cancellationRecords() *advancedmdtest.Adapter {
	records := advancedmdtest.NewAdapter()
	records.AppointmentResults["12345"] = appointmentResult(
		[]domain.PatientAppointment{cancellableAppointment()},
		true,
	)
	return records
}

func appointmentResult(
	appointments []domain.PatientAppointment,
	complete bool,
) advancedmdtest.AppointmentResult {
	return advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: appointments,
			Complete:     complete,
		},
	}
}

func cancellableAppointment() domain.PatientAppointment {
	return domain.PatientAppointment{
		ID:       33333,
		Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		OfficeID: "spring_hill",
		Office:   "Spring Hill",
	}
}

func cancellationCommand() scheduling.CancelCommand {
	return scheduling.CancelCommand{
		PatientID:     "pat12345",
		AppointmentID: 33333,
		Office:        "Spring Hill",
	}
}
