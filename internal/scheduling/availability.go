package scheduling

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

func (s *service) Search(ctx context.Context, command SearchCommand) (domain.AvailabilityResponse, error) {
	return s.search(ctx, command, 0)
}

// ListCommand loads a complete inventory window for conversational selection.
// Patient eligibility and booking policy are identical to Search.
type ListCommand struct {
	PatientID       string `json:"patientId,omitempty"`
	InsurancePlan   string `json:"insurancePlan,omitempty"`
	CoverageType    string `json:"coverageType,omitempty"`
	VisitType       string `json:"visitType,omitempty"`
	StartDate       string `json:"startDate,omitempty"`
	RangeDays       int    `json:"rangeDays,omitempty"`
	Office          string `json:"office"`
	DOB             string `json:"dob,omitempty"`
	Routing         string `json:"routing"`
	PreauthRequired bool   `json:"preauthRequired"`
}

func (s *service) List(ctx context.Context, command ListCommand) (domain.AvailabilityResponse, error) {
	days := command.RangeDays
	if days == 0 {
		days = 14
	}
	if days != 14 {
		return domain.AvailabilityResponse{}, schedulingError("rangeDays must be 14; use startDate to search a different window")
	}
	return s.search(ctx, SearchCommand{VisitType: command.VisitType, Office: command.Office, DOB: command.DOB,
		PatientID: command.PatientID, InsurancePlan: command.InsurancePlan, CoverageType: command.CoverageType,
		RequestedDate: command.StartDate, Routing: command.Routing, PreauthRequired: command.PreauthRequired}, days)
}

func (s *service) search(ctx context.Context, command SearchCommand, inventoryDays int) (domain.AvailabilityResponse, error) {
	empty := domain.AvailabilityResponse{}
	now := s.now()
	nowEastern := now.In(eastern)
	requestedDate := command.RequestedDate
	if requestedDate == "" {
		requestedDate = nowEastern.AddDate(0, 0, 1).Format("2006-01-02")
	}
	originalRequestedDate := requestedDate

	startDate, err := time.Parse("2006-01-02", requestedDate)
	if err != nil {
		return empty, schedulingError("Invalid date format. Use YYYY-MM-DD.")
	}
	if err := validatePreferredTime(command.PreferredTime); err != nil {
		return empty, schedulingError(err.Error())
	}
	hasPreference := inventoryDays == 0 && (command.RequestedDate != "" || command.PreferredTime != nil)
	if err := domain.ValidateOptionalDOB(command.DOB); err != nil {
		return empty, schedulingError(err.Error())
	}

	if startDate.Format("2006-01-02") <= nowEastern.Format("2006-01-02") {
		return empty, schedulingError("Same-day and past-date appointments are not available. Please search for tomorrow or later.")
	}
	if command.PreauthRequired {
		startDate = enforcePreauthMinDate(startDate, nowEastern)
	}
	searchStartDate := startDate.Format("2006-01-02")
	maxDate := startDate.AddDate(0, 0, searchForwardDays)
	if inventoryDays > 0 {
		maxDate = startDate.AddDate(0, 0, inventoryDays-1)
	}
	searchEndDate := maxDate.Format("2006-01-02")

	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		return empty, schedulingError(err.Error())
	}
	policy := domain.NewSchedulingPolicy(office)
	if command.VisitType != "" && command.VisitType != domain.AppointmentVisitMedical && command.VisitType != domain.AppointmentVisitRoutineVision {
		return empty, schedulingError("visitType must be medical or routine_vision")
	}
	unsupportedVisit := func() domain.AvailabilityResponse {
		return domain.AvailabilityResponse{
			Status: domain.AvailabilityStatusSuccess, Outcome: domain.AvailabilityOutcomeNoEligibleProviders,
			RequestedDate: originalRequestedDate, NextAction: domain.AvailabilityNextActionAskDifferentPreferences,
			Slots: []domain.AvailabilitySlotOption{}, Message: "This office or routing does not support the requested visit type.",
		}
	}
	if (command.VisitType == domain.AppointmentVisitMedical && !policy.SupportsMedical()) ||
		(command.VisitType == domain.AppointmentVisitRoutineVision && !policy.SupportsRouting(domain.RoutingOpticalOnly)) {
		return unsupportedVisit(), nil
	}
	// Legacy inventory consumers can omit patientId. Booking always rechecks
	// chart insurance; patient-scoped inventory additionally enforces it here.
	if command.PatientID != "" {
		coverage := command.CoverageType
		if coverage == "" {
			coverage = command.VisitType
		}
		if coverage == "" {
			coverage = "medical"
		}
		if command.VisitType != "" && coverage != command.VisitType {
			return empty, schedulingError("coverageType must match visitType")
		}
		insurance, err := s.insuranceForSearch(ctx, command.PatientID, command.InsurancePlan, coverage, office, command.DOB)
		if err != nil {
			return empty, err
		}
		command.Routing = string(insurance.Routing)
	}
	routing := policy.SchedulingRouting(domain.ParseRoutingRule(command.Routing), command.DOB)
	if (command.VisitType == domain.AppointmentVisitMedical && routing == domain.RoutingOpticalOnly) ||
		(command.VisitType == domain.AppointmentVisitRoutineVision && routing != domain.RoutingOpticalOnly) {
		return unsupportedVisit(), nil
	}

	setup, err := s.schedulerSetup(ctx, now.UTC())
	if err != nil {
		log.Printf("availability: scheduler setup failed category=%s", providerCategory(err))
		return empty, providerError(
			err,
			"Failed to load scheduler configuration from AdvancedMD. Please try again.",
		)
	}

	profileMap := make(map[string]domain.SchedulerProfile, len(setup.Profiles))
	for _, profile := range setup.Profiles {
		profileMap[profile.ID] = profile
	}
	allowedColumns := policy.EligibleColumns(setup.Columns, profileMap, routing, command.DOB, command.Provider)
	if len(allowedColumns) == 0 {
		if command.Provider != "" {
			return empty, schedulingError(fmt.Sprintf(
				"No provider found matching %q. Valid providers: %s",
				command.Provider,
				strings.Join(office.ValidProviderNames(), ", "),
			))
		}
		return domain.AvailabilityResponse{
			Status:                domain.AvailabilityStatusSuccess,
			Outcome:               domain.AvailabilityOutcomeNoEligibleProviders,
			AvailabilityFound:     false,
			RequestedDate:         originalRequestedDate,
			ShouldRetrySameSearch: false,
			NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
			Message:               "No eligible providers found for this office, routing, provider, and DOB.",
			Slots:                 []domain.AvailabilitySlotOption{},
		}, nil
	}

	var inventory map[string]domain.ScheduleReadResult
	if inventoryDays > 0 {
		inventory, err = s.readInventory(ctx, allowedColumns, startDate, maxDate)
		if err != nil {
			return empty, providerError(err, "Appointment scheduling is temporarily unavailable. Please try again.")
		}
	}
	var slots []domain.AvailabilitySlotOption
	searchIncomplete := false
	unavailableDataChecks := 0
	searchDate := startDate
	searchedThrough := searchStartDate
	var candidates []rankedAvailabilitySlot

	for !searchDate.After(maxDate) {
		date := searchDate.Format("2006-01-02")
		searchedThrough = date
		workingColumnIDs := make([]string, 0, len(allowedColumns))
		workingColumnSet := make(map[string]bool, len(allowedColumns))
		for _, column := range allowedColumns {
			if column.WorksOnDay(searchDate.Weekday()) {
				workingColumnIDs = append(workingColumnIDs, column.ID)
				workingColumnSet[column.ID] = true
			}
		}
		if len(workingColumnIDs) == 0 {
			searchDate = searchDate.AddDate(0, 0, 1)
			continue
		}

		read := inventory[date]
		var err error
		if inventoryDays == 0 {
			read, err = s.records.ReadSchedule(ctx, domain.ScheduleReadQuery{ColumnIDs: workingColumnIDs, Date: date})
		}
		if err != nil {
			log.Printf("availability: schedule read failed category=%s", providerCategory(err))
			return empty, providerError(
				err,
				"Appointment scheduling is temporarily unavailable. Please try again.",
			)
		}

		var daySlots []domain.AvailabilitySlotOption
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

			profile := profileMap[column.ProfileID]
			displayName := ""
			if officeColumn, ok := office.Columns[column.ID]; ok {
				displayName = officeColumn.DisplayName
			}
			if displayName == "" {
				displayName = office.ProviderDisplayName(column.ProfileID)
			}
			if displayName == "" {
				displayName = profile.Name
			}

			allSlots := availableSlots(policy, column, columnSchedule.Appointments, columnSchedule.BlockHolds, searchDate, nowEastern)
			if len(allSlots) == 0 {
				continue
			}
			columnID, _ := strconv.Atoi(column.ID)
			profileID, _ := strconv.Atoi(column.ProfileID)
			for _, slot := range allSlots {
				daySlots = append(daySlots, domain.AvailabilitySlotOption{
					Provider:          displayName,
					Time:              slot.Time,
					DateTime:          slot.DateTime,
					ColumnID:          columnID,
					ProfileID:         profileID,
					Duration:          column.Interval,
					SameStartBooked:   slot.SameStartBooked,
					SameStartCapacity: slot.SameStartCapacity,
					RequiresForce:     slot.RequiresForce,
				})
			}
		}

		sortAvailabilitySlots(daySlots)
		if inventoryDays > 0 {
			slots = append(slots, daySlots...)
		} else if !hasPreference {
			if len(daySlots) > 0 {
				slots = selectBroadAvailabilitySlots(daySlots)
				break
			}
		} else {
			for _, slot := range daySlots {
				ranked, err := rankedSlot(
					slot,
					command.RequestedDate,
					command.PreferredTime,
				)
				if err != nil {
					return empty, schedulingError("Failed to rank availability: " + err.Error())
				}
				candidates = append(candidates, ranked)
			}
			if !searchIncomplete && hasTwoExactAvailabilityMatches(candidates) {
				break
			}
		}
		searchDate = searchDate.AddDate(0, 0, 1)
	}

	if hasPreference {
		slots = selectPreferredAvailabilitySlots(candidates)
	}
	if inventoryDays > 0 && searchIncomplete {
		return incompleteResponse(originalRequestedDate, searchStartDate, searchEndDate, unavailableDataChecks), nil
	}
	if len(slots) == 0 {
		if searchIncomplete {
			return incompleteResponse(
				originalRequestedDate,
				searchStartDate,
				searchEndDate,
				unavailableDataChecks,
			), nil
		}
		return noneResponse(originalRequestedDate, searchStartDate, searchEndDate), nil
	}

	actualDate := slots[0].DateTime[:len("2006-01-02")]
	tokenIssuedAt := s.now().UTC()
	slots, tokenExpiresAt, err := s.signSlots(slots, office, routing, command.DOB, tokenIssuedAt)
	if err != nil {
		return empty, schedulingError("Failed to create booking tokens: " + err.Error())
	}
	return domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusSuccess,
		Outcome:               domain.AvailabilityOutcomeFound,
		AvailabilityFound:     true,
		RequestedDate:         originalRequestedDate,
		ShouldRetrySameSearch: false,
		NextAction:            domain.AvailabilityNextActionOfferSlots,
		ActualDate:            actualDate,
		DateShifted:           availabilityDateShifted(originalRequestedDate, searchStartDate, actualDate),
		SearchedFrom:          searchStartDate,
		SearchedThrough:       searchedThrough,
		BookingTokenExpiresAt: tokenExpiresAt.Format(time.RFC3339),
		Slots:                 slots,
	}, nil
}

func enforcePreauthMinDate(requestedDate, now time.Time) time.Time {
	// Match provider schedules: clinic calendar values encoded in UTC, not Eastern instants.
	minDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 14)
	if requestedDate.Before(minDate) {
		return minDate
	}
	return requestedDate
}

func availabilityDateShifted(requestedDate, searchStartDate, actualDate string) bool {
	if actualDate != "" {
		return actualDate != requestedDate
	}
	return searchStartDate != requestedDate
}

func noneResponse(requestedDate, searchStartDate, searchEndDate string) domain.AvailabilityResponse {
	return domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusSuccess,
		Outcome:               domain.AvailabilityOutcomeNoAvailability,
		AvailabilityFound:     false,
		RequestedDate:         requestedDate,
		ShouldRetrySameSearch: false,
		NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
		SearchedFrom:          searchStartDate,
		SearchedThrough:       searchEndDate,
		Message: fmt.Sprintf(
			"No availability was found from %s through %s. Do not search this same window again unless the patient changes date, provider, office, or appointment type.",
			searchStartDate,
			searchEndDate,
		),
		Slots: []domain.AvailabilitySlotOption{},
	}
}

func incompleteResponse(
	requestedDate,
	searchStartDate,
	searchEndDate string,
	unavailableDataChecks int,
) domain.AvailabilityResponse {
	return domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusError,
		Outcome:               domain.AvailabilityOutcomeSearchIncomplete,
		AvailabilityFound:     false,
		RequestedDate:         requestedDate,
		ShouldRetrySameSearch: true,
		NextAction:            domain.AvailabilityNextActionRetryOnceThenAskPreferences,
		SearchedFrom:          searchStartDate,
		SearchedThrough:       searchEndDate,
		Message: fmt.Sprintf(
			"Availability could not be fully checked from %s through %s because appointment data was unavailable for %d provider-date checks. Retry once; if it still cannot be checked, ask for different preferences.",
			searchStartDate,
			searchEndDate,
			unavailableDataChecks,
		),
		Slots: []domain.AvailabilitySlotOption{},
	}
}

var _ Scheduling = (*service)(nil)
