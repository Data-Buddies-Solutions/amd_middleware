package scheduling

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
)

const availabilityWindowDays = 14

type ListCommand struct {
	PatientID       string `json:"patientId,omitempty"`
	VisitType       string `json:"visitType,omitempty"`
	StartDate       string `json:"startDate,omitempty"`
	Office          string `json:"office"`
	DOB             string `json:"dob,omitempty"`
	Routing         string `json:"routing"`
	PreauthRequired bool   `json:"preauthRequired"`
	InsurancePlanID string `json:"insurancePlanId,omitempty"`
	RescheduleToken string `json:"rescheduleToken,omitempty"`
}

type moveSearch struct {
	appointmentID     int
	appointmentTypeID int
	doctorKey         string
	needsStaff        bool
}

func (s *service) List(ctx context.Context, command ListCommand) (AvailabilityResponse, error) {
	patientID, err := domain.ParsePatientID(command.PatientID)
	if err != nil {
		return AvailabilityResponse{}, schedulingError(err.Error())
	}
	var move moveSearch
	if command.RescheduleToken != "" {
		command, move, err = s.bindMoveSearch(command, patientID)
		if err != nil {
			return AvailabilityResponse{}, err
		}
		if move.needsStaff {
			return moveNeedsStaffResponse(command.StartDate, moveUnconfirmedMessage), nil
		}
	}
	if command.Routing == "" {
		command.Routing = string(domain.VisitRouting(command.VisitType))
	}
	return s.list(ctx, command, patientID, move)
}

func (s *service) bindMoveSearch(command ListCommand, patientID domain.PatientID) (ListCommand, moveSearch, error) {
	policy, err := s.appointmentTokens.verifyReschedule(command.RescheduleToken, s.now().UTC())
	if err != nil || patientID != policy.PatientID {
		return command, moveSearch{}, invalidRescheduleTokenError()
	}
	visit := domain.AppointmentVisitType(policy.AppointmentTypeID)
	planGated := command.InsurancePlanID != ""
	if visit == "" || (!planGated && policy.DoctorKey == "") {
		return command, moveSearch{needsStaff: true}, nil
	}
	if command.VisitType != "" && command.VisitType != visit {
		return command, moveSearch{}, schedulingError("visitType does not match the appointment being moved. A move keeps its visit type.")
	}
	command.VisitType = visit
	if planGated {
		if command.Office == "" {
			command.Office = policy.OfficeID
		}
		return command, moveSearch{appointmentID: policy.AppointmentID, appointmentTypeID: policy.AppointmentTypeID}, nil
	}
	if command.Office != "" {
		office, err := domain.ResolveOffice(command.Office)
		if err != nil {
			return command, moveSearch{}, schedulingError(err.Error())
		}
		if office.ID != policy.OfficeID {
			return command, moveSearch{}, schedulingError("office does not match the appointment being moved. Send insurancePlanId to search another office.")
		}
	}
	command.Office = policy.OfficeID
	return command, moveSearch{appointmentID: policy.AppointmentID, appointmentTypeID: policy.AppointmentTypeID, doctorKey: policy.DoctorKey}, nil
}

func (s *service) list(ctx context.Context, command ListCommand, patientID domain.PatientID, move moveSearch) (AvailabilityResponse, error) {
	empty := AvailabilityResponse{}
	now := s.now()
	nowEastern := now.In(eastern)
	requestedDate := command.StartDate
	if requestedDate == "" {
		requestedDate = nowEastern.AddDate(0, 0, 1).Format("2006-01-02")
	}
	originalRequestedDate := requestedDate

	startDate, err := time.Parse("2006-01-02", requestedDate)
	if err != nil {
		return empty, schedulingError("Invalid date format. Use YYYY-MM-DD.")
	}
	dob, err := domain.ParseDOB(command.DOB, now)
	if err != nil {
		return empty, schedulingError(err.Error())
	}
	requestedRouting, err := domain.ParseRoutingRule(command.Routing)
	if err != nil {
		return empty, schedulingError(err.Error())
	}

	if startDate.Format("2006-01-02") <= nowEastern.Format("2006-01-02") {
		return empty, schedulingError("Same-day and past-date appointments are not available. Please search for tomorrow or later.")
	}
	if command.PreauthRequired {
		startDate = enforcePreauthMinDate(startDate, nowEastern)
	}
	searchStartDate := startDate.Format("2006-01-02")
	maxDate := startDate.AddDate(0, 0, availabilityWindowDays-1)
	searchEndDate := maxDate.Format("2006-01-02")

	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return empty, schedulingError(err.Error())
	}
	policy := newSchedulingPolicy(office, now)
	if command.VisitType != "" && !domain.ValidVisitType(command.VisitType) {
		return empty, schedulingError("visitType must be medical or routine_vision")
	}
	var plan *insurance.InsuranceDecision
	if command.InsurancePlanID != "" {
		if command.VisitType == "" {
			return empty, schedulingError("visitType is required with insurancePlanId")
		}
		decision := insurance.DecidePlan(command.InsurancePlanID, command.VisitType, office, dob, now)
		if !decision.CanSchedule {
			return insuranceNotClearedResponse(originalRequestedDate, decision), nil
		}
		plan = &decision
	}
	unsupportedVisit := func() AvailabilityResponse {
		return AvailabilityResponse{
			Status: AvailabilityStatusSuccess, Outcome: AvailabilityOutcomeNoEligibleProviders,
			RequestedDate: originalRequestedDate, NextAction: AvailabilityNextActionAskDifferentPreferences,
			Slots: []AvailabilitySlotOption{}, Message: "This office or routing does not support the requested visit type.",
		}
	}
	if command.VisitType != "" && !office.SupportsVisit(command.VisitType) {
		return unsupportedVisit(), nil
	}
	if age, ok := dob.AgeOn(nowEastern); ok && command.VisitType == domain.AppointmentVisitRoutineVision && routineVisionTooYoung(office, age) {
		return empty, schedulingError(office.RoutineVisionMinimumAge.Message)
	}

	routing := office.SchedulingRouting(requestedRouting, dob, now)
	if (command.VisitType == domain.AppointmentVisitMedical && routing == domain.RoutingOpticalOnly) ||
		(command.VisitType == domain.AppointmentVisitRoutineVision && routing != domain.RoutingOpticalOnly) {
		return unsupportedVisit(), nil
	}
	if move.appointmentID != 0 && !office.AllowsAppointmentType(move.appointmentTypeID, routing) {
		return moveNeedsStaffResponse(originalRequestedDate, moveTypeNotAllowedMessage), nil
	}

	setup, err := s.schedulerSetup(ctx, now.UTC())
	if err != nil {
		log.Printf("availability: scheduler setup failed category=%s", advancedmd.CategoryOf(err))
		return empty, providerError(
			err,
			"Failed to load scheduler configuration from AdvancedMD. Please try again.",
		)
	}

	allowedColumns := policy.EligibleColumns(setup.Columns, routing, dob)
	if plan != nil {
		allowedColumns = columnsForProviders(office, allowedColumns, plan.AllowedProviders)
	}
	if move.doctorKey != "" {
		allowedColumns = columnsForDoctor(office, allowedColumns, move.doctorKey)
	}
	if len(allowedColumns) == 0 {
		return AvailabilityResponse{
			Status:                AvailabilityStatusSuccess,
			Outcome:               AvailabilityOutcomeNoEligibleProviders,
			AvailabilityFound:     false,
			RequestedDate:         originalRequestedDate,
			ShouldRetrySameSearch: false,
			NextAction:            AvailabilityNextActionAskDifferentPreferences,
			Message:               "No eligible providers found for this office, routing, and DOB.",
			Slots:                 []AvailabilitySlotOption{},
		}, nil
	}

	inventory, err := s.readInventory(ctx, allowedColumns, startDate, maxDate)
	if err != nil {
		return empty, providerError(err, "Appointment scheduling is temporarily unavailable. Please try again.")
	}
	var slots []AvailabilitySlotOption
	searchIncomplete := false
	unavailableDataChecks := 0
	searchDate := startDate
	for _, column := range allowedColumns {
		if !column.HasUsableSchedule() {
			searchIncomplete = true
			unavailableDataChecks++
		}
	}

	for !searchDate.After(maxDate) {
		date := searchDate.Format("2006-01-02")
		workingColumnSet := make(map[int]bool, len(allowedColumns))
		for _, column := range allowedColumns {
			if column.WorksOnDay(searchDate.Weekday()) {
				workingColumnSet[column.ID] = true
			}
		}
		if len(workingColumnSet) == 0 {
			searchDate = searchDate.AddDate(0, 0, 1)
			continue
		}

		read := inventory[date]
		var daySlots []AvailabilitySlotOption
		for _, column := range allowedColumns {
			if !workingColumnSet[column.ID] {
				continue
			}
			columnSchedule, ok := read.Columns[column.ID]
			if !ok || !columnSchedule.Complete() {
				searchIncomplete = true
				unavailableDataChecks++
				continue
			}

			displayName := office.Columns[column.ID].DisplayName
			for _, slot := range column.Slots(searchDate) {
				decision := policy.Slot(column.ID, slot, columnSchedule)
				if !decision.Bookable {
					continue
				}
				daySlots = append(daySlots, AvailabilitySlotOption{
					Provider:          displayName,
					Time:              formatSlotTime(slot.Start),
					DateTime:          domain.FormatSlotDateTime(slot.Start),
					ColumnID:          column.ID,
					ProfileID:         column.ProfileID,
					Duration:          column.Interval,
					SameStartBooked:   decision.Booked,
					SameStartCapacity: decision.Capacity,
					RequiresForce:     decision.RequiresForce,
				})
			}
		}

		sortAvailabilitySlots(daySlots)
		slots = append(slots, daySlots...)
		searchDate = searchDate.AddDate(0, 0, 1)
	}

	if searchIncomplete {
		return incompleteResponse(originalRequestedDate, searchStartDate, searchEndDate, unavailableDataChecks), nil
	}
	if len(slots) == 0 {
		return noneResponse(originalRequestedDate, searchStartDate, searchEndDate), nil
	}

	actualDate := slots[0].DateTime[:len("2006-01-02")]
	tokenIssuedAt := s.now().UTC()
	slots, tokenExpiresAt, err := s.signSlots(slots, office, routing, dob, slotClaims{patientID: patientID, moveOf: move.appointmentID}, tokenIssuedAt)
	if err != nil {
		return empty, schedulingError("Failed to create booking tokens: " + err.Error())
	}
	return AvailabilityResponse{
		Status:                AvailabilityStatusSuccess,
		Outcome:               AvailabilityOutcomeFound,
		AvailabilityFound:     true,
		RequestedDate:         originalRequestedDate,
		ShouldRetrySameSearch: false,
		NextAction:            availabilityNextActionOfferSlots,
		ActualDate:            actualDate,
		DateShifted:           actualDate != originalRequestedDate,
		SearchedFrom:          searchStartDate,
		SearchedThrough:       searchEndDate,
		BookingTokenExpiresAt: tokenExpiresAt.Format(time.RFC3339),
		Slots:                 slots,
	}, nil
}

func enforcePreauthMinDate(requestedDate, now time.Time) time.Time {
	minDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 14)
	if requestedDate.Before(minDate) {
		return minDate
	}
	return requestedDate
}

func insuranceNotClearedResponse(requestedDate string, decision insurance.InsuranceDecision) AvailabilityResponse {
	return AvailabilityResponse{
		Status:            AvailabilityStatusSuccess,
		Outcome:           AvailabilityOutcomeInsuranceNotCleared,
		RequestedDate:     requestedDate,
		NextAction:        AvailabilityNextActionStaffHelp,
		Message:           decision.Answer,
		Slots:             []AvailabilitySlotOption{},
		InsuranceDecision: &decision,
	}
}

func columnsForProviders(office *domain.OfficeConfig, columns []domain.SchedulerColumn, providers []string) []domain.SchedulerColumn {
	kept := make([]domain.SchedulerColumn, 0, len(columns))
	for _, column := range columns {
		if slices.Contains(providers, office.Columns[column.ID].ShortName) {
			kept = append(kept, column)
		}
	}
	return kept
}

func columnsForDoctor(office *domain.OfficeConfig, columns []domain.SchedulerColumn, doctorKey string) []domain.SchedulerColumn {
	kept := make([]domain.SchedulerColumn, 0, len(columns))
	for _, column := range columns {
		if office.Columns[column.ID].ProviderKey == doctorKey {
			kept = append(kept, column)
		}
	}
	return kept
}

const (
	moveUnconfirmedMessage    = "This appointment's doctor or visit type could not be confirmed, so openings cannot be searched for this move. Offer to have office staff move it."
	moveTypeNotAllowedMessage = "This office cannot take this patient's existing appointment type, so openings cannot be searched here for this move. Offer to have office staff move it."
)

func moveNeedsStaffResponse(requestedDate, message string) AvailabilityResponse {
	return AvailabilityResponse{
		Status:        AvailabilityStatusSuccess,
		Outcome:       AvailabilityOutcomeMoveNeedsStaff,
		RequestedDate: requestedDate,
		NextAction:    AvailabilityNextActionStaffHelp,
		Message:       message,
		Slots:         []AvailabilitySlotOption{},
	}
}

func noneResponse(requestedDate, searchStartDate, searchEndDate string) AvailabilityResponse {
	return AvailabilityResponse{
		Status:                AvailabilityStatusSuccess,
		Outcome:               AvailabilityOutcomeNoAvailability,
		AvailabilityFound:     false,
		RequestedDate:         requestedDate,
		ShouldRetrySameSearch: false,
		NextAction:            AvailabilityNextActionAskDifferentPreferences,
		SearchedFrom:          searchStartDate,
		SearchedThrough:       searchEndDate,
		Message: fmt.Sprintf(
			"No availability was found from %s through %s. Do not search this same window again unless the patient changes date, provider, office, or appointment type.",
			searchStartDate,
			searchEndDate,
		),
		Slots: []AvailabilitySlotOption{},
	}
}

func incompleteResponse(
	requestedDate,
	searchStartDate,
	searchEndDate string,
	unavailableDataChecks int,
) AvailabilityResponse {
	return AvailabilityResponse{
		Status:                AvailabilityStatusError,
		Outcome:               AvailabilityOutcomeSearchIncomplete,
		AvailabilityFound:     false,
		RequestedDate:         requestedDate,
		ShouldRetrySameSearch: true,
		NextAction:            AvailabilityNextActionRetryOnceThenAskPreferences,
		SearchedFrom:          searchStartDate,
		SearchedThrough:       searchEndDate,
		Message: fmt.Sprintf(
			"Availability could not be fully checked from %s through %s because appointment data was unavailable for %d provider-date checks. Retry once; if it still cannot be checked, contact office staff.",
			searchStartDate,
			searchEndDate,
			unavailableDataChecks,
		),
		Slots: []AvailabilitySlotOption{},
	}
}

var _ Scheduling = (*service)(nil)
