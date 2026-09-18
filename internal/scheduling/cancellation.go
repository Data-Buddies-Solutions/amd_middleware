package scheduling

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

// CancelCommand preserves the public cancellation request while Scheduling
// owns patient ownership and reconciliation.
type CancelCommand struct {
	AppointmentID     int
	PatientID         string
	Office            string
	CancellationToken *string
}

// CancelReceipt is the stable cancellation result returned to HTTP callers.
type CancelReceipt struct {
	Status        string `json:"status"`
	Outcome       string `json:"outcome,omitempty"`
	AppointmentID int    `json:"appointmentId,omitempty"`
	Message       string `json:"message"`
}

type cancellationTelemetry struct {
	path              string
	scheduleReads     int
	providerMutations int
	startedAt         time.Time
}

// CancellationObservation is the PHI-free operation budget for one
// cancellation request.
type CancellationObservation struct {
	Path              string
	Outcome           string
	ScheduleReads     int
	ProviderMutations int
	DurationMS        int64
}

type cancellationObserverKey struct{}

// WithCancellationObserver attaches a request-scoped telemetry sink.
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
		Path:              t.path,
		Outcome:           outcome,
		ScheduleReads:     t.scheduleReads,
		ProviderMutations: t.providerMutations,
		DurationMS:        time.Since(t.startedAt).Milliseconds(),
	})
}

func (s *service) Cancel(ctx context.Context, command CancelCommand) (receipt CancelReceipt, err error) {
	tokenProvided := command.CancellationToken != nil
	telemetry := cancellationTelemetry{path: "legacy", startedAt: time.Now()}
	if tokenProvided {
		telemetry.path = "token"
	}
	defer func() {
		telemetry.record(ctx, err)
	}()

	if tokenProvided {
		return s.cancelWithToken(ctx, command, &telemetry)
	}
	if command.AppointmentID == 0 {
		return CancelReceipt{}, schedulingError("appointmentId is required")
	}
	command.PatientID = domain.StripPatientPrefix(strings.TrimSpace(command.PatientID))
	if command.PatientID == "" {
		return CancelReceipt{}, schedulingError("patientId is required")
	}
	if _, err := strconv.Atoi(command.PatientID); err != nil {
		return CancelReceipt{}, schedulingError("patientId must be numeric")
	}
	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return CancelReceipt{}, schedulingError(err.Error())
	}
	if s.records == nil {
		return CancelReceipt{}, ownershipCheckError()
	}

	read, err := s.records.ReadPatientAppointments(ctx, domain.PatientAppointmentsQuery{
		PatientID: command.PatientID,
		OfficeIDs: domain.AppointmentLookupOfficeIDs(office),
	})
	telemetry.scheduleReads += read.ProviderReads
	if err != nil {
		return CancelReceipt{}, ownershipCheckError()
	}
	appointment, owningOffice, found, knownOffice := ownedAppointment(
		read.Appointments,
		command.AppointmentID,
		office,
	)
	if !knownOffice {
		return CancelReceipt{}, ownershipCheckError()
	}
	if !found {
		if !read.Complete {
			return CancelReceipt{}, ownershipCheckError()
		}
		return CancelReceipt{}, categorizedError(
			CategoryOwnershipMismatch,
			"No upcoming appointment matches that patient and appointment ID. Please load appointments again and choose the appointment to cancel.",
		)
	}

	return s.cancelVerifiedAppointment(
		ctx,
		advancedmd.Cancellation{
			PatientID:     command.PatientID,
			AppointmentID: command.AppointmentID,
			OfficeID:      owningOffice.ID,
		},
		appointment,
		owningOffice,
		&telemetry,
	)
}

func (s *service) cancelWithToken(
	ctx context.Context,
	command CancelCommand,
	telemetry *cancellationTelemetry,
) (CancelReceipt, error) {
	policy, err := s.appointmentTokens.verify(*command.CancellationToken, s.now().UTC())
	if err != nil {
		return CancelReceipt{}, invalidCancellationTokenError()
	}
	if command.PatientID != "" {
		patientID := domain.StripPatientPrefix(strings.TrimSpace(command.PatientID))
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
	if s.records == nil {
		return CancelReceipt{}, categorizedError(
			CategoryWriteFailed,
			"Appointment scheduling is temporarily unavailable. Please try again.",
		)
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
	providerFailure := providerCategory(err)
	switch providerFailure {
	case safeerrors.CategoryConflict:
		return categorizedProviderError(
			CategoryProviderConflict,
			providerFailure,
			"AdvancedMD could not cancel the appointment because its state changed. Please load appointments again.",
		)
	case safeerrors.CategoryRejected:
		return categorizedProviderError(
			CategoryProviderRejected,
			providerFailure,
			"AdvancedMD rejected the cancellation. Please load appointments again or contact the office.",
		)
	case safeerrors.CategoryAuthentication, safeerrors.CategoryUnavailable:
		return categorizedProviderError(
			CategoryWriteFailed,
			providerFailure,
			"Service authentication is temporarily unavailable. Please try again.",
		)
	default:
		return categorizedProviderError(
			CategoryWriteFailed,
			providerFailure,
			"Failed to cancel appointment in AdvancedMD. Please try again or contact the office.",
		)
	}
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

func ownedAppointment(
	appointments []domain.PatientAppointment,
	appointmentID int,
	requestedOffice *domain.OfficeConfig,
) (domain.PatientAppointment, *domain.OfficeConfig, bool, bool) {
	for _, appointment := range appointments {
		if appointment.ID != appointmentID {
			continue
		}
		if appointment.OfficeID == "" {
			return appointment, requestedOffice, true, true
		}
		office, ok := domain.LookupOfficeByID(appointment.OfficeID)
		return appointment, office, true, ok
	}
	return domain.PatientAppointment{}, nil, false, true
}

func cancellationReceipt(appointmentID int) CancelReceipt {
	return CancelReceipt{
		Status:        "cancelled",
		AppointmentID: appointmentID,
		Message:       "Appointment cancelled successfully",
	}
}
