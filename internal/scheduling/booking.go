package scheduling

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const maxAppointmentCommentLength = 1000

// BookCommand preserves the public booking request while Scheduling owns its
// validation, provider write, reconciliation, and receipt.
type BookCommand struct {
	PatientID         string `json:"patientId"`
	PatientName       string `json:"patientName,omitempty"`
	DOB               string `json:"dob,omitempty"`
	BookingToken      string `json:"bookingToken,omitempty"`
	RescheduleToken   string `json:"rescheduleToken,omitempty"`
	ColumnID          int    `json:"columnId"`
	ProfileID         int    `json:"profileId"`
	StartDatetime     string `json:"startDatetime"`
	Duration          int    `json:"duration"`
	AppointmentTypeID int    `json:"appointmentTypeId"`
	Routing           string `json:"routing,omitempty"`
	Office            string `json:"office,omitempty"`
	VisitCategory     string `json:"visitCategory,omitempty"`
	VisitKind         string `json:"visitKind,omitempty"`
	PatientStatus     string `json:"patientStatus,omitempty"`
	AgeBand           string `json:"ageBand,omitempty"`
	IsPostOp          bool   `json:"isPostOp,omitempty"`
	VisitReason       string `json:"visitReason,omitempty"`
	AppointmentReason string `json:"appointmentReason,omitempty"`
	ReferringDoctor   string `json:"referringDoctor,omitempty"`
}

// BookReceipt is the stable booking result returned to HTTP callers.
type BookReceipt struct {
	Status              string   `json:"status"`
	Outcome             string   `json:"outcome,omitempty"`
	AppointmentID       int      `json:"appointmentId,omitempty"`
	PatientID           string   `json:"patientId,omitempty"`
	PatientName         string   `json:"patientName,omitempty"`
	ProviderName        string   `json:"providerName,omitempty"`
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
	command                 BookCommand
	office                  *domain.OfficeConfig
	patientID               int
	start                   time.Time
	force                   bool
	comments                string
	providerAppointmentType int
	appointmentColor        string
}

type bookingContext struct {
	command                  BookCommand
	office                   *domain.OfficeConfig
	token                    SlotPolicy
	signed                   bool
	preservedAppointmentType int
}

func (s *service) Book(ctx context.Context, command BookCommand) (BookReceipt, error) {
	prepared, err := s.prepareBooking(ctx, command)
	if err != nil {
		return BookReceipt{}, err
	}

	appointmentID, err := s.records.BookAppointment(ctx, advancedmd.Booking{
		PatientID:                 prepared.patientID,
		OfficeID:                  prepared.office.ID,
		ColumnID:                  prepared.command.ColumnID,
		ProfileID:                 prepared.command.ProfileID,
		Start:                     prepared.start,
		Duration:                  prepared.command.Duration,
		AppointmentTypeID:         prepared.command.AppointmentTypeID,
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

	providerFailure := providerCategory(err)
	switch providerFailure {
	case safeerrors.CategoryConflict:
		return BookReceipt{}, categorizedProviderError(
			CategorySlotUnavailable,
			providerFailure,
			"This time slot is no longer available. Please check availability again and choose a different slot.",
		)
	case safeerrors.CategoryRejected:
		return BookReceipt{}, categorizedProviderError(
			CategoryProviderRejected,
			providerFailure,
			"AdvancedMD rejected the booking. Please check availability again or contact the office.",
		)
	case safeerrors.CategoryAuthentication, safeerrors.CategoryUnavailable:
		return BookReceipt{}, categorizedProviderError(
			CategoryWriteFailed,
			providerFailure,
			"Service authentication is temporarily unavailable. Please try again.",
		)
	default:
		return BookReceipt{}, categorizedProviderError(
			CategoryWriteFailed,
			providerFailure,
			"Failed to book appointment in AdvancedMD. Please try again or contact the office.",
		)
	}
}

func (s *service) prepareBooking(ctx context.Context, command BookCommand) (preparedBooking, error) {
	booking, err := s.resolveBookingContext(command)
	if err != nil {
		return preparedBooking{}, err
	}
	patientID, err := s.verifyBookingPatient(ctx, &booking)
	if err != nil {
		return preparedBooking{}, err
	}
	policy, decision, comments, err := applyBookingPolicy(&booking)
	if err != nil {
		return preparedBooking{}, err
	}
	start, force, err := s.revalidateBookingSlot(ctx, booking, policy)
	if err != nil {
		return preparedBooking{}, err
	}
	return preparedBooking{
		command:                 booking.command,
		office:                  booking.office,
		patientID:               patientID,
		start:                   start,
		force:                   force,
		comments:                comments,
		providerAppointmentType: decision.EnvironmentTypeID,
		appointmentColor:        decision.Color,
	}, nil
}

func (s *service) resolveBookingContext(command BookCommand) (bookingContext, error) {
	booking := bookingContext{
		command: command,
		signed:  command.BookingToken != "",
	}
	if !booking.signed && !s.allowRawBooking {
		return bookingContext{}, categorizedError(
			CategoryBookingTokenRequired,
			"bookingToken is required. Please check availability again and choose one of the returned slots.",
		)
	}

	if booking.signed {
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
		if token.DOB != "" && command.DOB != "" && domain.NormalizeDOB(command.DOB) != token.DOB {
			return bookingContext{}, invalidBookingTokenError()
		}
		if token.DOB != "" {
			command.DOB = token.DOB
		}
		command.ColumnID = token.ColumnID
		command.ProfileID = token.ProfileID
		command.StartDatetime = token.StartDatetime
		command.Duration = token.Duration
		command.Routing = token.Routing
		booking.token = token
		booking.office = office
	} else {
		office, err := domain.ResolveOffice(command.Office)
		if err != nil {
			return bookingContext{}, schedulingError(err.Error())
		}
		booking.office = office
	}
	if command.RescheduleToken != "" {
		policy, err := s.appointmentTokens.verifyReschedule(command.RescheduleToken, s.now().UTC())
		if err != nil || domain.StripPatientPrefix(strings.TrimSpace(command.PatientID)) != policy.PatientID {
			return bookingContext{}, invalidRescheduleTokenError()
		}
		command.AppointmentTypeID = policy.AppointmentTypeID
		booking.preservedAppointmentType = policy.AppointmentTypeID
	}
	booking.command = command

	if command.PatientID == "" {
		return bookingContext{}, schedulingError("patientId is required")
	}
	if command.ColumnID == 0 {
		return bookingContext{}, schedulingError("columnId is required")
	}
	if command.ProfileID == 0 {
		return bookingContext{}, schedulingError("profileId is required")
	}
	if command.StartDatetime == "" {
		return bookingContext{}, schedulingError("startDatetime is required")
	}
	if command.Duration == 0 {
		return bookingContext{}, schedulingError("duration is required")
	}
	if _, err := strconv.Atoi(command.PatientID); err != nil {
		return bookingContext{}, schedulingError("patientId must be numeric")
	}
	if err := domain.ValidateOptionalDOB(command.DOB); err != nil {
		return bookingContext{}, schedulingError(err.Error())
	}
	return booking, nil
}

func (s *service) verifyBookingPatient(ctx context.Context, booking *bookingContext) (int, error) {
	if s.records == nil {
		return 0, categorizedError(
			CategoryWriteFailed,
			"Appointment scheduling is temporarily unavailable. Please try again.",
		)
	}

	demographics, err := s.records.GetPatientDemographics(ctx, booking.command.PatientID)
	if err != nil {
		return 0, categorizedError(
			CategoryWriteFailed,
			"Unable to verify patient before booking. Please try again.",
		)
	}
	if demographics.DOB == "" || domain.ValidateOptionalDOB(demographics.DOB) != nil {
		return 0, categorizedError(
			CategoryWriteFailed,
			"Unable to verify patient before booking. Please try again.",
		)
	}
	verifiedDOB := domain.NormalizeDOB(demographics.DOB)
	expectedDOB := booking.command.DOB
	if booking.signed {
		expectedDOB = booking.token.DOB
	}
	if expectedDOB != "" && verifiedDOB != domain.NormalizeDOB(expectedDOB) {
		return 0, categorizedError(
			CategoryPatientContextMismatch,
			"The selected slot does not match the verified patient. Please check availability again.",
		)
	}
	booking.command.DOB = verifiedDOB

	patientID, err := strconv.Atoi(booking.command.PatientID)
	if err != nil {
		return 0, schedulingError("patientId must be numeric")
	}
	return patientID, nil
}

func applyBookingPolicy(booking *bookingContext) (
	domain.SchedulingPolicy,
	domain.BookingPolicyDecision,
	string,
	error,
) {
	command := booking.command
	comments := buildAppointmentComment(command.AppointmentReason, command.ReferringDoctor)
	if len([]rune(comments)) > maxAppointmentCommentLength {
		return domain.SchedulingPolicy{}, domain.BookingPolicyDecision{}, "", schedulingError(
			fmt.Sprintf("appointment comments must be %d characters or fewer", maxAppointmentCommentLength),
		)
	}

	policy := domain.NewSchedulingPolicy(booking.office)
	decision, policyErr := policy.PrepareBooking(domain.BookingPolicyRequest{
		ColumnID:          command.ColumnID,
		ProfileID:         command.ProfileID,
		AppointmentTypeID: command.AppointmentTypeID,
		Routing:           domain.ParseRoutingRule(command.Routing),
		DOB:               command.DOB,
		Intent: domain.AppointmentIntent{
			VisitCategory: command.VisitCategory,
			VisitKind:     command.VisitKind,
			PatientStatus: command.PatientStatus,
			AgeBand:       command.AgeBand,
			DOB:           command.DOB,
			IsPostOp:      command.IsPostOp,
			VisitReason:   command.VisitReason,
		},
		PreservedAppointmentTypeID: booking.preservedAppointmentType,
	})
	if policyErr != nil {
		category := CategoryValidation
		if policyErr.Outcome == string(CategoryAppointmentTypeMissing) {
			category = CategoryAppointmentTypeMissing
		}
		return domain.SchedulingPolicy{}, domain.BookingPolicyDecision{}, "", &Error{
			category: category,
			message:  policyErr.Message,
			missing:  append([]string(nil), policyErr.Missing...),
		}
	}
	if booking.signed &&
		booking.preservedAppointmentType == 0 &&
		!slices.Contains(booking.token.AppointmentTypeIDs, decision.AppointmentTypeID) {
		return domain.SchedulingPolicy{}, domain.BookingPolicyDecision{}, "", invalidBookingTokenError()
	}
	command.Routing = string(decision.Routing)
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

func (s *service) revalidateBookingSlot(
	ctx context.Context,
	booking bookingContext,
	policy domain.SchedulingPolicy,
) (time.Time, bool, error) {
	command := booking.command
	setup, err := s.records.GetSchedulerSetup(ctx)
	if err != nil {
		return time.Time{}, false, categorizedError(
			CategoryWriteFailed,
			"Unable to revalidate the selected provider. Please check availability again.",
		)
	}
	column, ok := currentBookingColumn(&setup, booking.office, command)
	if !ok {
		return time.Time{}, false, categorizedError(
			CategorySlotUnavailable,
			"The selected provider is no longer eligible for this slot. Please check availability again.",
		)
	}

	start, err := time.Parse("2006-01-02T15:04", command.StartDatetime)
	if err != nil {
		if booking.signed {
			return time.Time{}, false, invalidBookingTokenError()
		}
		return time.Time{}, false, schedulingError("startDatetime must use YYYY-MM-DDTHH:MM format")
	}
	duration := time.Duration(command.Duration) * time.Minute
	if !currentColumnSupportsSlot(column, start, duration) {
		return time.Time{}, false, slotUnavailableError()
	}
	read, err := s.records.ReadSchedule(ctx, domain.ScheduleReadQuery{
		ColumnIDs: []string{strconv.Itoa(command.ColumnID)},
		Date:      start.Format("2006-01-02"),
	})
	if err != nil {
		return time.Time{}, false, categorizedError(
			CategoryWriteFailed,
			"Unable to revalidate the selected slot. Please check availability again.",
		)
	}
	schedule := read.Columns[strconv.Itoa(command.ColumnID)]
	if !schedule.Complete() ||
		domain.IsBlockedByHold(start, duration, schedule.BlockHolds) ||
		hasDifferentStartOverlap(start, duration, schedule.Appointments) {
		return time.Time{}, false, slotUnavailableError()
	}

	booked := countSameStart(start, schedule.Appointments)
	sameStart := policy.SameStart(strconv.Itoa(command.ColumnID), start, booked)
	if !sameStart.Bookable {
		return time.Time{}, false, slotUnavailableError()
	}
	if booking.signed {
		if booking.token.SameStartBooked != booked ||
			booking.token.SameStartCapacity != sameStart.Capacity ||
			booking.token.RequiresForce != sameStart.RequiresForce {
			return time.Time{}, false, slotUnavailableError()
		}
	} else if sameStart.RequiresForce {
		return time.Time{}, false, slotUnavailableError()
	}

	return start, sameStart.RequiresForce, nil
}

func (s *service) reconcileBooking(ctx context.Context, prepared preparedBooking) (BookReceipt, error) {
	read, err := s.records.ReadPatientAppointmentsForMonth(ctx, advancedmd.AppointmentMonthQuery{
		PatientID: prepared.command.PatientID,
		OfficeIDs: domain.AppointmentLookupOfficeIDs(prepared.office),
		Month:     prepared.start,
	})
	if err != nil {
		log.Printf("booking: reconciliation outcome=indeterminate category=%s", providerCategory(err))
		return BookReceipt{}, categorizedError(
			CategoryIndeterminateWrite,
			"AdvancedMD may have booked the appointment, but the result could not be verified. Do not retry automatically; load appointments before taking another action.",
		)
	}

	provider := prepared.office.Columns[strconv.Itoa(prepared.command.ColumnID)].DisplayName
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
	office *domain.OfficeConfig,
	command BookCommand,
) (domain.SchedulerColumn, bool) {
	if setup == nil {
		return domain.SchedulerColumn{}, false
	}
	profiles := make(map[string]domain.SchedulerProfile, len(setup.Profiles))
	for _, profile := range setup.Profiles {
		profiles[profile.ID] = profile
	}
	eligible := domain.NewSchedulingPolicy(office).EligibleColumns(
		setup.Columns,
		profiles,
		domain.ParseRoutingRule(command.Routing),
		command.DOB,
		"",
	)
	for _, column := range eligible {
		if column.ID == strconv.Itoa(command.ColumnID) &&
			column.ProfileID == strconv.Itoa(command.ProfileID) &&
			column.FacilityID == office.FacilityID {
			return column, true
		}
	}
	return domain.SchedulerColumn{}, false
}

func currentColumnSupportsSlot(column domain.SchedulerColumn, start time.Time, duration time.Duration) bool {
	if !column.WorksOnDay(start.Weekday()) || column.Interval <= 0 {
		return false
	}
	workStart, workEnd, err := column.ParseWorkHours(start)
	if err != nil {
		return false
	}
	interval := time.Duration(column.Interval) * time.Minute
	return duration == interval &&
		!start.Before(workStart) &&
		!start.Add(duration).After(workEnd) &&
		start.Sub(workStart)%interval == 0
}

func buildBookReceipt(command BookCommand, office *domain.OfficeConfig, appointmentID int) BookReceipt {
	column := office.Columns[strconv.Itoa(command.ColumnID)]
	appointmentTypeName, _ := office.AppointmentTypeName(command.AppointmentTypeID)
	return BookReceipt{
		Status:              "booked",
		AppointmentID:       appointmentID,
		PatientID:           command.PatientID,
		PatientName:         normalizePatientName(command.PatientName),
		ProviderName:        column.DisplayName,
		LocationName:        office.DisplayName,
		StartDatetime:       command.StartDatetime,
		Duration:            command.Duration,
		AppointmentTypeID:   command.AppointmentTypeID,
		AppointmentTypeName: appointmentTypeName,
		Message:             "Appointment booked successfully",
	}
}

func (s *service) buildBookReceipt(prepared preparedBooking, appointmentID int) BookReceipt {
	receipt := buildBookReceipt(prepared.command, prepared.office, appointmentID)
	token, err := s.appointmentTokens.IssueRescheduleToken(
		prepared.command.PatientID,
		domain.PatientAppointment{
			ID:                appointmentID,
			Start:             prepared.start,
			AppointmentTypeID: prepared.command.AppointmentTypeID,
			OfficeID:          prepared.office.ID,
		},
	)
	if err == nil {
		receipt.RescheduleToken = token
	}
	return receipt
}

func normalizePatientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if parts := strings.SplitN(name, ",", 2); len(parts) == 2 {
		name = strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])
	}
	if name == strings.ToUpper(name) || name == strings.ToLower(name) {
		return cases.Title(language.English).String(strings.ToLower(name))
	}
	return name
}

func buildAppointmentComment(reason, referringDoctor string) string {
	reason = strings.TrimSpace(reason)
	referringDoctor = strings.TrimSpace(referringDoctor)
	if reason == "" && referringDoctor == "" {
		return ""
	}
	if reason == "" {
		reason = "none"
	}
	if referringDoctor == "" {
		referringDoctor = "none"
	}
	return strings.Join([]string{
		"Appointment reason: " + reason,
		"Referring doctor: " + referringDoctor,
		"- AI",
	}, "\n")
}
