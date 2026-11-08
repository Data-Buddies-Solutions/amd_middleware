package scheduling

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const maxAppointmentCommentLength = 1000

type BookCommand struct {
	PatientID              string `json:"patientId"`
	PatientName            string `json:"patientName,omitempty"`
	DOB                    string `json:"dob,omitempty"`
	BookingToken           string `json:"bookingToken,omitempty"`
	RescheduleToken        string `json:"rescheduleToken,omitempty"`
	AppointmentTypeID      int    `json:"appointmentTypeId"`
	Office                 string `json:"office,omitempty"`
	VisitCategory          string `json:"visitCategory,omitempty"`
	VisitReason            string `json:"visitReason,omitempty"`
	AppointmentReason      string `json:"appointmentReason,omitempty"`
	ReferringDoctor        string `json:"referringDoctor,omitempty"`
	ReferringDoctorPhone   string `json:"referringDoctorPhone,omitempty"`
	ReferringDoctorAddress string `json:"referringDoctorAddress,omitempty"`
}

type BookReceipt struct {
	OfficeID            string   `json:"officeId,omitempty"`
	Office              string   `json:"office,omitempty"`
	VisitType           string   `json:"visitType,omitempty"`
	CancellationToken   string   `json:"cancellationToken,omitempty"`
	Status              string   `json:"status"`
	Outcome             string   `json:"outcome,omitempty"`
	AppointmentID       int      `json:"appointmentId,omitempty"`
	PatientID           string   `json:"patientId,omitempty"`
	PatientName         string   `json:"patientName,omitempty"`
	ProviderName        string   `json:"providerName,omitempty"`
	ProfileID           string   `json:"profileId,omitempty"`
	LocationName        string   `json:"locationName,omitempty"`
	StartDatetime       string   `json:"startDatetime,omitempty"`
	Duration            int      `json:"duration,omitempty"`
	AppointmentTypeID   int      `json:"appointmentTypeId,omitempty"`
	AppointmentTypeName string   `json:"appointmentTypeName,omitempty"`
	RescheduleToken     string   `json:"rescheduleToken,omitempty"`
	Message             string   `json:"message"`
	Missing             []string `json:"missing,omitempty"`
}

type preparedBooking struct {
	token                   SlotPolicy
	command                 BookCommand
	office                  *domain.OfficeConfig
	patientID               domain.PatientID
	start                   time.Time
	force                   bool
	comments                string
	providerAppointmentType int
	appointmentColor        string
}

type bookingContext struct {
	command                  BookCommand
	patientID                domain.PatientID
	dob                      domain.DOB
	office                   *domain.OfficeConfig
	token                    SlotPolicy
	preservedAppointmentType int
	patientStatus            string
	asOf                     time.Time
}

func (s *service) Book(ctx context.Context, command BookCommand) (BookReceipt, error) {
	if command.RescheduleToken != "" {
		return BookReceipt{}, schedulingError("rescheduleToken is accepted only when rescheduling. Booking creates a new visit.")
	}
	booking, err := s.resolveBookingContext(command, 0)
	if err != nil {
		return BookReceipt{}, err
	}
	if booking.token.MoveOf != 0 {
		return BookReceipt{}, categorizedError(
			CategoryInvalidBookingToken,
			"This slot was offered to move an existing appointment, so it cannot book a new one. Please check availability again for a new visit.",
		)
	}
	return s.book(ctx, booking)
}

func (s *service) book(ctx context.Context, booking bookingContext) (BookReceipt, error) {
	prepared, err := s.prepareBooking(ctx, booking)
	if err != nil {
		return BookReceipt{}, err
	}

	appointmentID, err := s.records.BookAppointment(ctx, advancedmd.Booking{
		PatientID:                 prepared.patientID.Int(),
		FacilityID:                prepared.office.FacilityID,
		ColumnID:                  prepared.token.ColumnID,
		ProfileID:                 prepared.token.ProfileID,
		Start:                     prepared.start,
		Duration:                  prepared.token.Duration,
		ProviderAppointmentTypeID: prepared.providerAppointmentType,
		AppointmentColor:          prepared.appointmentColor,
		Force:                     prepared.force,
		Comments:                  prepared.comments,
	})
	if err == nil {
		return s.buildBookReceipt(prepared, appointmentID), nil
	}
	if advancedmd.IsAmbiguousWrite(err) {
		return s.reconcileBooking(ctx, prepared)
	}

	return BookReceipt{}, providerWriteError(err, CategorySlotUnavailable,
		"This time slot is no longer available. Please check availability again and choose a different slot.",
		"AdvancedMD rejected the booking. Please check availability again or contact the office.",
		"Failed to book appointment in AdvancedMD. Please try again or contact the office.",
	)
}

type bookingReads struct {
	running        *sync.WaitGroup
	completedVisit func() (bool, error)
	setup          func() (*domain.SchedulerSetup, error)
	day            func() (domain.ColumnSchedule, error)
}

func (s *service) startBookingReads(ctx context.Context, booking bookingContext) bookingReads {
	running := &sync.WaitGroup{}
	reads := bookingReads{
		running: running,
		setup: startRead(running, func() (*domain.SchedulerSetup, error) {
			return s.schedulerSetup(ctx, s.now().UTC())
		}),
		day: startRead(running, func() (domain.ColumnSchedule, error) {
			start, err := time.Parse(domain.SlotDateTimeLayout, booking.token.StartDatetime)
			if err != nil {
				return domain.ColumnSchedule{}, err
			}
			return s.records.ReadColumnDay(ctx, booking.token.ColumnID, start.Format("2006-01-02"))
		}),
	}
	if booking.command.AppointmentTypeID == 0 {
		reads.completedVisit = startRead(running, func() (bool, error) {
			return s.records.HasCompletedVisit(ctx, string(booking.patientID))
		})
	}
	return reads
}

func startRead[T any](running *sync.WaitGroup, read func() (T, error)) func() (T, error) {
	var value T
	var err error
	var recovered any
	done := make(chan struct{})
	running.Go(func() {
		defer close(done)
		defer func() {
			if recovered = recover(); recovered != nil {
				log.Printf("booking: pre-write read panicked type=%T", recovered)
			}
		}()
		value, err = read()
	})
	return func() (T, error) {
		<-done
		if recovered != nil {
			panic(recovered)
		}
		return value, err
	}
}

func (s *service) prepareBooking(ctx context.Context, booking bookingContext) (preparedBooking, error) {
	ctx, cancel := context.WithCancel(ctx)
	reads := s.startBookingReads(ctx, booking)
	defer reads.running.Wait()
	defer cancel()
	if err := s.verifyBookingPatient(ctx, &booking); err != nil {
		return preparedBooking{}, err
	}
	if err := classifyBookingPatient(&booking, reads.completedVisit); err != nil {
		return preparedBooking{}, err
	}
	policy, decision, comments, err := applyBookingPolicy(&booking)
	if err != nil {
		return preparedBooking{}, err
	}
	start, force, err := revalidateBookingSlot(booking, policy, reads)
	if err != nil {
		return preparedBooking{}, err
	}
	return preparedBooking{
		command:                 booking.command,
		token:                   booking.token,
		office:                  booking.office,
		patientID:               booking.patientID,
		start:                   start,
		force:                   force,
		comments:                comments,
		providerAppointmentType: decision.EnvironmentTypeID,
		appointmentColor:        decision.Color,
	}, nil
}

func (s *service) resolveBookingContext(command BookCommand, preservedAppointmentType int) (bookingContext, error) {
	patientID, patientIDErr := domain.ParsePatientID(command.PatientID)
	booking := bookingContext{
		command:                  command,
		preservedAppointmentType: preservedAppointmentType,
		asOf:                     s.now(),
	}
	if command.BookingToken == "" {
		return bookingContext{}, categorizedError(
			CategoryBookingTokenRequired,
			"bookingToken is required. Please check availability again and choose one of the returned slots.",
		)
	}

	token, err := VerifySlotToken(s.bookingTokenSecret, command.BookingToken, s.now().UTC())
	if err != nil {
		return bookingContext{}, invalidBookingTokenError()
	}
	office, ok := domain.LookupOfficeByID(token.OfficeID)
	if !ok {
		return bookingContext{}, invalidBookingTokenError()
	}
	if command.Office != "" {
		requestedOffice, err := domain.ResolveOffice(command.Office)
		if err != nil || requestedOffice.ID != office.ID {
			return bookingContext{}, invalidBookingTokenError()
		}
	}
	dob, dobErr := domain.ParseDOB(command.DOB, booking.asOf)
	if token.DOB != "" && command.DOB != "" && (dobErr != nil || dob != token.DOB) {
		return bookingContext{}, invalidBookingTokenError()
	}
	if token.DOB != "" {
		dob, dobErr = token.DOB, nil
	}
	booking.token = token
	booking.office = office

	if patientIDErr != nil {
		return bookingContext{}, schedulingError(patientIDErr.Error())
	}
	if patientID == "" {
		return bookingContext{}, schedulingError("patientId is required")
	}
	if token.PatientID == "" {
		return bookingContext{}, categorizedError(
			CategoryInvalidBookingToken,
			"This slot was not offered for a specific patient. Please check availability again.",
		)
	}
	if token.PatientID != patientID {
		return bookingContext{}, categorizedError(
			CategoryPatientContextMismatch,
			"The selected slot does not match the verified patient. Please check availability again.",
		)
	}
	if dobErr != nil {
		return bookingContext{}, schedulingError(dobErr.Error())
	}
	booking.patientID = patientID
	booking.dob = dob
	if (command.VisitCategory != "" || command.AppointmentTypeID == 0) && !domain.ValidVisitType(command.VisitCategory) {
		return bookingContext{}, schedulingError("visitCategory must be medical or routine_vision")
	}
	return booking, nil
}

func (s *service) verifyBookingPatient(ctx context.Context, booking *bookingContext) error {
	demographics, err := s.records.GetPatientDemographics(ctx, string(booking.patientID))
	if err != nil {
		return categorizedProviderError(
			CategoryWriteFailed,
			advancedmd.CategoryOf(err),
			"Unable to verify patient before booking. Please try again.",
		)
	}
	verifiedDOB, err := domain.ParseDOB(demographics.DOB, booking.asOf)
	if err != nil || verifiedDOB == "" {
		return categorizedError(
			CategoryWriteFailed,
			"Unable to verify patient before booking. Please try again.",
		)
	}
	if booking.token.DOB != "" && verifiedDOB != booking.token.DOB {
		return categorizedError(
			CategoryPatientContextMismatch,
			"The selected slot does not match the verified patient. Please check availability again.",
		)
	}
	booking.dob = verifiedDOB
	return nil
}

func classifyBookingPatient(booking *bookingContext, completedVisit func() (bool, error)) error {
	if completedVisit == nil {
		return nil
	}
	seen, err := completedVisit()
	if err != nil {
		return categorizedProviderError(
			CategoryWriteFailed,
			advancedmd.CategoryOf(err),
			"Unable to verify patient before booking. Please try again.",
		)
	}
	booking.patientStatus = domain.AppointmentPatientNew
	if seen {
		booking.patientStatus = domain.AppointmentPatientEstablished
	}
	return nil
}

func applyBookingPolicy(booking *bookingContext) (
	schedulingPolicy,
	bookingPolicyDecision,
	string,
	error,
) {
	command := booking.command
	comments := buildAppointmentComment(command)
	if len([]rune(comments)) > maxAppointmentCommentLength {
		return schedulingPolicy{}, bookingPolicyDecision{}, "", schedulingError(
			fmt.Sprintf("appointment comments must be %d characters or fewer", maxAppointmentCommentLength),
		)
	}

	policy := newSchedulingPolicy(booking.office, booking.asOf)
	decision, policyErr := policy.PrepareBooking(bookingPolicyRequest{
		ColumnID:          booking.token.ColumnID,
		ProfileID:         booking.token.ProfileID,
		AppointmentTypeID: command.AppointmentTypeID,
		Routing:           booking.token.Routing,
		DOB:               booking.dob,
		Intent: appointmentIntent{
			VisitCategory: command.VisitCategory,
			PatientStatus: booking.patientStatus,
			VisitReason:   command.VisitReason,
		},
		PreservedAppointmentTypeID: booking.preservedAppointmentType,
	})
	if policyErr != nil {
		category := CategoryValidation
		if policyErr.Outcome == string(categoryAppointmentTypeMissing) {
			category = categoryAppointmentTypeMissing
		}
		return schedulingPolicy{}, bookingPolicyDecision{}, "", &Error{
			category: category,
			message:  policyErr.Message,
			missing:  append([]string(nil), policyErr.Missing...),
		}
	}
	if booking.preservedAppointmentType == 0 &&
		!slices.Contains(booking.token.AppointmentTypeIDs, decision.AppointmentTypeID) {
		return schedulingPolicy{}, bookingPolicyDecision{}, "", invalidBookingTokenError()
	}
	booking.token.Routing = decision.Routing
	command.AppointmentTypeID = decision.AppointmentTypeID
	booking.command = command
	return policy, decision, comments, nil
}

func invalidRescheduleTokenError() error {
	return categorizedError(
		CategoryInvalidRescheduleToken,
		"rescheduleToken is invalid or expired. Load appointments again before rescheduling.",
	)
}

func revalidateBookingSlot(
	booking bookingContext,
	policy schedulingPolicy,
	reads bookingReads,
) (time.Time, bool, error) {
	setup, err := reads.setup()
	if err != nil {
		return time.Time{}, false, categorizedProviderError(
			CategoryWriteFailed,
			advancedmd.CategoryOf(err),
			"Unable to revalidate the selected provider. Please check availability again.",
		)
	}
	column, ok := currentBookingColumn(setup, booking)
	if !ok {
		return time.Time{}, false, categorizedError(
			CategorySlotUnavailable,
			"The selected provider is no longer eligible for this slot. Please check availability again.",
		)
	}

	start, err := time.Parse(domain.SlotDateTimeLayout, booking.token.StartDatetime)
	if err != nil {
		return time.Time{}, false, invalidBookingTokenError()
	}
	slot := domain.Interval{Start: start, End: start.Add(time.Duration(booking.token.Duration) * time.Minute)}
	if !slices.ContainsFunc(column.Slots(start), slot.Equal) {
		return time.Time{}, false, slotUnavailableError()
	}
	schedule, err := reads.day()
	if err != nil {
		return time.Time{}, false, categorizedProviderError(
			CategoryWriteFailed,
			advancedmd.CategoryOf(err),
			"Unable to revalidate the selected slot. Please check availability again.",
		)
	}
	signed := slotDecision{
		Booked:        booking.token.SameStartBooked,
		Capacity:      booking.token.SameStartCapacity,
		Bookable:      true,
		RequiresForce: booking.token.RequiresForce,
	}
	if policy.Slot(column.ID, slot, schedule) != signed {
		return time.Time{}, false, slotUnavailableError()
	}

	return start, signed.RequiresForce, nil
}

func (s *service) reconcileBooking(ctx context.Context, prepared preparedBooking) (BookReceipt, error) {
	read, err := s.records.ReadPatientAppointmentsForDay(ctx, advancedmd.AppointmentDayQuery{
		PatientID: string(prepared.patientID),
		Offices:   prepared.office.AppointmentLookupOffices,
		Day:       prepared.start,
	})
	if err != nil {
		log.Printf("booking: reconciliation outcome=indeterminate category=%s", advancedmd.CategoryOf(err))
		return BookReceipt{}, categorizedError(
			CategoryIndeterminateWrite,
			"AdvancedMD may have booked the appointment, but the result could not be verified. Do not retry automatically; load appointments before taking another action.",
		)
	}

	provider := prepared.office.Columns[prepared.token.ColumnID].DisplayName
	for _, appointment := range read.Appointments {
		if appointment.Start.Equal(prepared.start) &&
			appointment.OfficeID == prepared.office.ID &&
			appointment.Provider == provider &&
			appointment.AppointmentTypeID == prepared.command.AppointmentTypeID {
			log.Printf("booking: reconciliation outcome=success")
			return s.buildBookReceipt(prepared, appointment.ID), nil
		}
	}
	if !read.Complete {
		log.Printf("booking: reconciliation outcome=indeterminate category=incomplete_read")
		return BookReceipt{}, categorizedError(
			CategoryIndeterminateWrite,
			"AdvancedMD may have booked the appointment, but the result could not be verified. Do not retry automatically; load appointments before taking another action.",
		)
	}

	log.Printf("booking: reconciliation outcome=failure")
	return BookReceipt{}, categorizedError(
		CategoryWriteFailed,
		"AdvancedMD did not create the appointment. Please check availability again before retrying.",
	)
}

func invalidBookingTokenError() error {
	return categorizedError(
		CategoryInvalidBookingToken,
		"Invalid or expired booking token. Please check availability again and choose a slot.",
	)
}

func slotUnavailableError() error {
	return categorizedError(
		CategorySlotUnavailable,
		"This time slot is no longer available. Please check availability again and choose a different slot.",
	)
}

func currentBookingColumn(
	setup *domain.SchedulerSetup,
	booking bookingContext,
) (domain.SchedulerColumn, bool) {
	if setup == nil {
		return domain.SchedulerColumn{}, false
	}
	eligible := newSchedulingPolicy(booking.office, booking.asOf).EligibleColumns(
		setup.Columns,
		booking.token.Routing,
		booking.dob,
	)
	for _, column := range eligible {
		if column.ID == booking.token.ColumnID &&
			column.ProfileID == booking.token.ProfileID &&
			column.FacilityID == booking.office.FacilityID {
			return column, true
		}
	}
	return domain.SchedulerColumn{}, false
}

func buildBookReceipt(prepared preparedBooking, appointmentID int) BookReceipt {
	command, office := prepared.command, prepared.office
	column := office.Columns[prepared.token.ColumnID]
	appointmentTypeName, _ := office.AppointmentTypeName(command.AppointmentTypeID)
	return BookReceipt{
		Status:              "booked",
		OfficeID:            office.ID,
		Office:              office.DisplayName,
		VisitType:           domain.AppointmentVisitType(command.AppointmentTypeID),
		AppointmentID:       appointmentID,
		PatientID:           string(prepared.patientID),
		PatientName:         normalizePatientName(command.PatientName),
		ProviderName:        column.DisplayName,
		ProfileID:           strconv.Itoa(prepared.token.ProfileID),
		LocationName:        office.DisplayName,
		StartDatetime:       prepared.token.StartDatetime,
		Duration:            prepared.token.Duration,
		AppointmentTypeID:   command.AppointmentTypeID,
		AppointmentTypeName: appointmentTypeName,
		Message:             "Appointment booked successfully",
	}
}

func (s *service) buildBookReceipt(prepared preparedBooking, appointmentID int) BookReceipt {
	receipt := buildBookReceipt(prepared, appointmentID)
	appointment := domain.PatientAppointment{
		ID: appointmentID, ColumnID: prepared.token.ColumnID, Start: prepared.start,
		AppointmentTypeID: prepared.command.AppointmentTypeID, OfficeID: prepared.office.ID,
	}
	receipt.RescheduleToken, _ = s.appointmentTokens.IssueRescheduleToken(string(prepared.patientID), appointment)
	receipt.CancellationToken, _ = s.appointmentTokens.IssueCancellationToken(string(prepared.patientID), appointment)

	return receipt
}

func normalizePatientName(full string) string {
	recorded := domain.ParseRecordName(full)
	name := strings.TrimSpace(recorded.Given + " " + recorded.Last)
	if name == "" {
		return ""
	}
	if name == strings.ToUpper(name) || name == strings.ToLower(name) {
		return cases.Title(language.English).String(strings.ToLower(name))
	}
	return name
}

func buildAppointmentComment(command BookCommand) string {
	reason := strings.TrimSpace(command.AppointmentReason)
	referringDoctor := strings.TrimSpace(command.ReferringDoctor)
	if reason == "" && referringDoctor == "" {
		return ""
	}
	if reason == "" {
		reason = "none"
	}
	if referringDoctor == "" {
		referringDoctor = "none"
	}
	lines := []string{
		"Appointment reason: " + reason,
		"Referring doctor: " + referringDoctor,
	}
	if phone := strings.TrimSpace(command.ReferringDoctorPhone); phone != "" {
		lines = append(lines, "Referring doctor phone: "+phone)
	}
	if address := strings.TrimSpace(command.ReferringDoctorAddress); address != "" {
		lines = append(lines, "Referring doctor address: "+address)
	}
	return strings.Join(append(lines, "- AI"), "\n")
}
