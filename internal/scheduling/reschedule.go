package scheduling

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

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

// RescheduleRecord is durable before the first provider mutation. Claims never
// expire or transfer to another worker: a crashed write needs staff reconciliation.
type RescheduleRecord struct {
	// Recovery coordinates remain available even if the process dies before a
	// provider receipt is returned, without retaining the caller-supplied tokens.
	PatientID             string            `json:"patientId"`
	OriginalAppointmentID int               `json:"originalAppointmentId"`
	OriginalOfficeID      string            `json:"originalOfficeId"`
	OriginalStart         string            `json:"originalStart"`
	ReplacementOfficeID   string            `json:"replacementOfficeId"`
	ReplacementStart      string            `json:"replacementStart"`
	ReplacementProfileID  int               `json:"replacementProfileId"`
	StartedAt             time.Time         `json:"startedAt"`
	Selection             string            `json:"selection"`
	Receipt               RescheduleReceipt `json:"receipt"`
}

type RescheduleStore interface {
	// Claim atomically creates a record or returns the existing record. On any
	// ambiguous storage error the caller must not start a provider mutation.
	Claim(context.Context, string, RescheduleRecord) (existing RescheduleRecord, created bool, err error)
	Save(context.Context, string, RescheduleRecord) error
}

func digest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

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
	if s.reschedules == nil {
		return RescheduleReceipt{}, categorizedError(CategoryWriteFailed, "Rescheduling is unavailable until durable recovery storage is configured. No appointment was changed.")
	}
	key := digest([]any{policy.OfficeID, policy.PatientID, policy.AppointmentID, policy.Start})
	// Refreshed tokens carry the same semantic selection. A changed destination
	// must not turn a retry of this original appointment into a second booking.
	selection := digest([]any{booking.office.ID, booking.command.ColumnID, booking.command.ProfileID, booking.command.StartDatetime, booking.command.Duration})
	record := RescheduleRecord{
		PatientID: policy.PatientID, OriginalAppointmentID: policy.AppointmentID,
		OriginalOfficeID: policy.OfficeID, OriginalStart: policy.Start,
		ReplacementOfficeID: booking.office.ID, ReplacementStart: booking.command.StartDatetime,
		ReplacementProfileID: booking.command.ProfileID, StartedAt: s.now().UTC(),
		Selection: selection, Receipt: RescheduleReceipt{
			Status: "uncertain", Outcome: string(CategoryIndeterminateWrite),
			Message: "A reschedule is in progress or requires reconciliation. Do not repeat provider writes; ask staff to reconcile the original and replacement appointments.",
		}}
	existing, created, err := s.reschedules.Claim(ctx, key, record)
	if err != nil {
		return RescheduleReceipt{}, categorizedError(CategoryWriteFailed, "Unable to claim the reschedule safely. No provider writes were started by this request.")
	}
	if !created {
		if existing.Selection != selection {
			return RescheduleReceipt{Status: "uncertain", Outcome: "reschedule_conflict", Booking: existing.Receipt.Booking, Cancellation: existing.Receipt.Cancellation, Message: "This original appointment already has a different reschedule attempt. Reconcile that result before making another change."}, nil
		}
		return existing.Receipt, nil
	}
	// Once claimed, finish reconciliation even if the HTTP caller disconnects.
	// The bounded context also prevents an abandoned worker from running forever.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	finish := func(receipt RescheduleReceipt) (RescheduleReceipt, error) {
		record.Receipt = receipt
		if err := s.reschedules.Save(ctx, key, record); err != nil {
			// The original claim still prevents replay. Keep any proven effects visible.
			receipt.Message += " Recovery receipt persistence failed; staff must reconcile before any further change."
		}
		return receipt, nil
	}
	// Re-read the original before any write; signed tokens are authorization, not
	// proof that an appointment still exists with the same time and type.
	read, err := s.records.ReadPatientAppointmentsForMonth(ctx, advancedmd.AppointmentMonthQuery{
		PatientID: policy.PatientID, OfficeIDs: []string{policy.OfficeID}, Month: policy.start,
	})
	if err != nil || !read.Complete {
		return finish(RescheduleReceipt{Status: "failed", Outcome: string(CategoryWriteFailed), Message: "Unable to verify the original appointment. No appointment was changed."})
	}
	found := false
	for _, a := range read.Appointments {
		if a.ID == policy.AppointmentID && a.OfficeID == policy.OfficeID && a.Start.Equal(policy.start) && a.AppointmentTypeID == policy.AppointmentTypeID {
			found = true
		}
	}
	if !found {
		return finish(RescheduleReceipt{Status: "failed", Outcome: string(CategoryOwnershipMismatch), Message: "The original appointment changed or no longer exists. No appointment was changed."})
	}
	replacement, err := s.Book(ctx, command)
	if err != nil {
		status := "failed"
		if CategoryOf(err) == CategoryIndeterminateWrite {
			status = "uncertain"
		}
		return finish(RescheduleReceipt{Status: status, Outcome: string(CategoryOf(err)), Message: err.Error() + " The original appointment was not cancelled."})
	}
	if replacement.AppointmentID <= 0 || replacement.AppointmentID == policy.AppointmentID {
		return finish(RescheduleReceipt{Status: "uncertain", Outcome: string(CategoryIndeterminateWrite), Message: "The replacement identity could not be verified. The original was not cancelled; ask staff to reconcile."})
	}
	partial := RescheduleReceipt{Status: "partial", Booking: &replacement, Message: "The replacement is booked. The original cancellation is not confirmed. Do not book again; ask staff to reconcile."}
	record.Receipt = partial
	// Persist the replacement before cancelling. Failure here must stop the second
	// write; retries can only observe the claim, never create another replacement.
	if err := s.reschedules.Save(ctx, key, record); err != nil {
		return partial, nil
	}
	// Staff may have changed the original while the replacement was being booked.
	// Do not cancel an appointment whose current identity no longer matches the
	// confirmed token, even when its numeric provider ID is unchanged.
	current, readErr := s.records.ReadPatientAppointmentsForMonth(ctx, advancedmd.AppointmentMonthQuery{
		PatientID: policy.PatientID, OfficeIDs: []string{policy.OfficeID}, Month: policy.start,
	})
	unchanged := false
	if readErr == nil && current.Complete {
		for _, a := range current.Appointments {
			if a.ID == policy.AppointmentID && a.OfficeID == policy.OfficeID && a.Start.Equal(policy.start) && a.AppointmentTypeID == policy.AppointmentTypeID {
				unchanged = true
			}
		}
	}
	if !unchanged {
		partial.Outcome = string(CategoryOwnershipMismatch)
		return finish(partial)
	}
	office, _ := domain.LookupOfficeByID(policy.OfficeID)
	cancellation, err := s.cancelVerifiedAppointment(ctx, advancedmd.Cancellation{
		PatientID: policy.PatientID, AppointmentID: policy.AppointmentID, OfficeID: policy.OfficeID,
	}, domain.PatientAppointment{ID: policy.AppointmentID, OfficeID: policy.OfficeID, Start: policy.start}, office, &cancellationTelemetry{})
	if err != nil {
		partial.Outcome = string(CategoryOf(err))
		return finish(partial)
	}
	return finish(RescheduleReceipt{Status: "completed", Booking: &replacement, Cancellation: &cancellation, Message: "The replacement is booked and the original appointment is cancelled."})
}
