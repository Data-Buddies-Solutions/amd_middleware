package scheduling

import (
	"context"
	"log"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
)

type CancelCommand struct {
	AppointmentID     int
	PatientID         string
	Office            string
	CancellationToken string
}

type CancelReceipt struct {
	Status        string `json:"status"`
	Outcome       string `json:"outcome,omitempty"`
	AppointmentID int    `json:"appointmentId,omitempty"`
	Message       string `json:"message"`
}

type cancellationTelemetry struct {
	scheduleReads     int
	providerMutations int
	startedAt         time.Time
}

type CancellationObservation struct {
	Outcome           string
	ScheduleReads     int
	ProviderMutations int
	DurationMS        int64
}

type cancellationObserverKey struct{}

func WithCancellationObserver(
	ctx context.Context,
	observer func(CancellationObservation),
) context.Context {
	return context.WithValue(ctx, cancellationObserverKey{}, observer)
}

func (t cancellationTelemetry) record(ctx context.Context, err error) {
	observer, ok := ctx.Value(cancellationObserverKey{}).(func(CancellationObservation))
	if !ok {
		return
	}
	outcome := "success"
	if err != nil {
		outcome = string(CategoryOf(err))
	}
	observer(CancellationObservation{
		Outcome:           outcome,
		ScheduleReads:     t.scheduleReads,
		ProviderMutations: t.providerMutations,
		DurationMS:        time.Since(t.startedAt).Milliseconds(),
	})
}

func (s *service) Cancel(ctx context.Context, command CancelCommand) (receipt CancelReceipt, err error) {
	telemetry := cancellationTelemetry{startedAt: time.Now()}
	defer func() { telemetry.record(ctx, err) }()
	if command.CancellationToken == "" {
		return CancelReceipt{}, schedulingError("cancellationToken is required")
	}
	return s.cancelWithToken(ctx, command, &telemetry)
}

func (s *service) cancelWithToken(
	ctx context.Context,
	command CancelCommand,
	telemetry *cancellationTelemetry,
) (CancelReceipt, error) {
	policy, err := s.appointmentTokens.verifyCancellation(command.CancellationToken, s.now().UTC())
	if err != nil {
		return CancelReceipt{}, invalidCancellationTokenError()
	}
	if command.PatientID != "" {
		patientID := domain.StripPatientPrefix(command.PatientID)
		if patientID != policy.PatientID {
			return CancelReceipt{}, invalidCancellationTokenError()
		}
	}
	if command.AppointmentID != 0 && command.AppointmentID != policy.AppointmentID {
		return CancelReceipt{}, invalidCancellationTokenError()
	}
	office, ok := domain.LookupOfficeByID(policy.OfficeID)
	if !ok {
		return CancelReceipt{}, invalidCancellationTokenError()
	}
	if command.Office != "" {
		requestedOffice, err := domain.ResolveOffice(command.Office)
		if err != nil || requestedOffice.ID != office.ID {
			return CancelReceipt{}, invalidCancellationTokenError()
		}
	}
	return s.cancelVerifiedAppointment(
		ctx,
		advancedmd.Cancellation{
			PatientID:     policy.PatientID,
			AppointmentID: policy.AppointmentID,
			OfficeID:      policy.OfficeID,
		},
		domain.PatientAppointment{
			ID:       policy.AppointmentID,
			Start:    policy.start,
			OfficeID: policy.OfficeID,
		},
		office,
		telemetry,
	)
}

func (s *service) cancelVerifiedAppointment(
	ctx context.Context,
	cancellation advancedmd.Cancellation,
	appointment domain.PatientAppointment,
	office *domain.OfficeConfig,
	telemetry *cancellationTelemetry,
) (CancelReceipt, error) {
	telemetry.providerMutations++
	err := s.records.CancelAppointment(ctx, cancellation)
	if err == nil {
		return cancellationReceipt(cancellation.AppointmentID), nil
	}
	if advancedmd.IsAmbiguousWrite(err) {
		telemetry.scheduleReads++
		return s.reconcileCancellation(
			ctx,
			cancellation.AppointmentID,
			appointment,
			office,
		)
	}
	return CancelReceipt{}, cancellationProviderError(err)
}

func cancellationProviderError(err error) error {
	return providerWriteError(err, CategoryProviderConflict,
		"AdvancedMD could not cancel the appointment because its state changed. Please load appointments again.",
		"AdvancedMD rejected the cancellation. Please load appointments again or contact the office.",
		"Failed to cancel appointment in AdvancedMD. Please try again or contact the office.",
	)
}

func invalidCancellationTokenError() error {
	return categorizedError(
		CategoryInvalidCancellationToken,
		"cancellationToken is invalid or expired. Please load appointments again and choose the appointment to cancel.",
	)
}

func (s *service) reconcileCancellation(
	ctx context.Context,
	appointmentID int,
	appointment domain.PatientAppointment,
	office *domain.OfficeConfig,
) (CancelReceipt, error) {
	state, err := s.records.ReadAppointmentState(ctx, advancedmd.AppointmentStateQuery{
		AppointmentID: appointmentID,
		OfficeID:      office.ID,
		Start:         appointment.Start,
	})
	if err != nil {
		log.Printf("cancellation: reconciliation outcome=indeterminate category=%s", providerCategory(err))
		return CancelReceipt{}, categorizedError(
			CategoryIndeterminateWrite,
			"AdvancedMD may have cancelled the appointment, but the result could not be verified. Do not retry automatically; load appointments before taking another action.",
		)
	}
	if state.Exists {
		log.Printf("cancellation: reconciliation outcome=failure")
		return CancelReceipt{}, categorizedError(
			CategoryWriteFailed,
			"AdvancedMD did not cancel the appointment. Please load appointments again before retrying.",
		)
	}
	if !state.Complete {
		log.Printf("cancellation: reconciliation outcome=indeterminate category=incomplete_read")
		return CancelReceipt{}, categorizedError(
			CategoryIndeterminateWrite,
			"AdvancedMD may have cancelled the appointment, but the result could not be verified. Do not retry automatically; load appointments before taking another action.",
		)
	}

	log.Printf("cancellation: reconciliation outcome=success")
	return cancellationReceipt(appointmentID), nil
}

func ownershipCheckError() error {
	return categorizedError(
		CategoryWriteFailed,
		"Unable to verify appointment before cancellation. Please load appointments again and choose the appointment to cancel.",
	)
}

func cancellationReceipt(appointmentID int) CancelReceipt {
	return CancelReceipt{
		Status:        "cancelled",
		AppointmentID: appointmentID,
		Message:       "Appointment cancelled successfully",
	}
}
