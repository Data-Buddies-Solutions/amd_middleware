package scheduling

import (
	"context"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
)

// RescheduleReceipt reports the two independent provider effects. A partial or
// uncertain result must never be presented as a completed move.
type RescheduleReceipt struct {
	Status       string         `json:"status"`
	Outcome      string         `json:"outcome,omitempty"`
	Booking      *BookReceipt   `json:"booking,omitempty"`
	Cancellation *CancelReceipt `json:"cancellation,omitempty"`
	Message      string         `json:"message"`
}

// Reschedule sends each provider write once. The caller owns replay protection
// and must reconcile uncertain or partial results before another mutation.
func (s *service) Reschedule(ctx context.Context, command BookCommand) (RescheduleReceipt, error) {
	if command.BookingToken == "" {
		return RescheduleReceipt{}, categorizedError(CategoryBookingTokenRequired, "A current bookingToken is required to reschedule.")
	}
	if s.records == nil {
		return RescheduleReceipt{}, categorizedError(CategoryWriteFailed, "Scheduling is unavailable. No appointment was changed.")
	}
	policy, err := s.appointmentTokens.verifyReschedule(command.RescheduleToken, s.now().UTC())
	if err != nil || command.PatientID != policy.PatientID || policy.AppointmentTypeID == 0 {
		return RescheduleReceipt{}, invalidRescheduleTokenError()
	}
	booking, err := s.resolveBookingContext(command)
	if err != nil {
		return RescheduleReceipt{}, err
	}
	visit := domain.AppointmentVisitType(policy.AppointmentTypeID)
	if visit == "" || (command.VisitCategory != "" && command.VisitCategory != visit) {
		return RescheduleReceipt{}, schedulingError("The existing visit type cannot be preserved. Reload appointments or ask staff for help.")
	}
	// The preserved type must obey destination office and lane restrictions too.
	if !booking.office.AllowsAppointmentType(policy.AppointmentTypeID, domain.ParseRoutingRule(booking.command.Routing)) {
		return RescheduleReceipt{}, schedulingError("The destination does not support the existing appointment type.")
	}
	if err := s.verifyRescheduleOriginal(ctx, policy); err != nil {
		return RescheduleReceipt{}, err
	}
	replacement, err := s.Book(ctx, command)
	if err != nil {
		status := "failed"
		if CategoryOf(err) == CategoryIndeterminateWrite {
			status = "uncertain"
		}
		return RescheduleReceipt{Status: status, Outcome: string(CategoryOf(err)), Message: err.Error() + " The original appointment was not cancelled."}, nil
	}
	if replacement.AppointmentID <= 0 || replacement.AppointmentID == policy.AppointmentID {
		return RescheduleReceipt{Status: "uncertain", Outcome: string(CategoryIndeterminateWrite), Message: "The replacement identity could not be verified. The original was not cancelled; ask staff to reconcile."}, nil
	}
	partial := RescheduleReceipt{Status: "partial", Booking: &replacement, Message: "The replacement is booked. The original cancellation is not confirmed. Do not book again; ask staff to reconcile."}
	// The original may have changed while booking. Keep the replacement and
	// return partial instead of cancelling an appointment the caller did not confirm.
	if err := s.verifyRescheduleOriginal(ctx, policy); err != nil {
		partial.Outcome = string(CategoryOf(err))
		return partial, nil
	}
	office, _ := domain.LookupOfficeByID(policy.OfficeID)
	cancellation, err := s.cancelVerifiedAppointment(ctx, advancedmd.Cancellation{
		PatientID: policy.PatientID, AppointmentID: policy.AppointmentID, OfficeID: policy.OfficeID,
	}, domain.PatientAppointment{ID: policy.AppointmentID, OfficeID: policy.OfficeID, Start: policy.start}, office, &cancellationTelemetry{})
	if err != nil {
		partial.Outcome = string(CategoryOf(err))
		return partial, nil
	}
	return RescheduleReceipt{Status: "completed", Booking: &replacement, Cancellation: &cancellation, Message: "The replacement is booked and the original appointment is cancelled."}, nil
}

// Tokens authorize a confirmed appointment; a fresh read verifies it still
// exists with the same identity before either provider write.
func (s *service) verifyRescheduleOriginal(ctx context.Context, policy appointmentTokenPolicy) error {
	read, err := s.records.ReadPatientAppointmentsForMonth(ctx, advancedmd.AppointmentMonthQuery{
		PatientID: policy.PatientID, OfficeIDs: []string{policy.OfficeID}, Month: policy.start,
	})
	if err != nil || !read.Complete {
		return ownershipCheckError()
	}
	for _, appointment := range read.Appointments {
		if appointment.ID == policy.AppointmentID && appointment.OfficeID == policy.OfficeID && appointment.Start.Equal(policy.start) && appointment.AppointmentTypeID == policy.AppointmentTypeID {
			return nil
		}
	}
	return categorizedError(CategoryOwnershipMismatch, "The original appointment changed or no longer exists. Load appointments again before another change.")
}
