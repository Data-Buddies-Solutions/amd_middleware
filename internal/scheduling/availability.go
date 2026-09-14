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
	plan, err := s.prepareAvailability(ctx, command, searchForwardDays+1)
	if err != nil {
		return domain.AvailabilityResponse{}, err
	}
	var slots []domain.AvailabilitySlotOption
	var candidates []rankedAvailabilitySlot
	unavailable := 0
	searchedThrough := plan.end.Format("2006-01-02")
	hasPreference := command.RequestedDate != "" || command.PreferredTime != nil
	for _, query := range plan.queries() {
		read, err := s.records.ReadSchedule(ctx, query)
		if err != nil {
			return domain.AvailabilityResponse{}, providerError(err, "Appointment scheduling is temporarily unavailable. Please try again.")
		}
		daySlots, missing := plan.slots(query, read)
		unavailable += missing
		if !hasPreference {
			if len(daySlots) > 0 {
				slots = selectBroadAvailabilitySlots(daySlots)
				searchedThrough = query.Date
				break
			}
			continue
		}
		for _, slot := range daySlots {
			ranked, err := rankedSlot(slot, command.RequestedDate, command.PreferredTime)
			if err != nil {
				return domain.AvailabilityResponse{}, schedulingError("Failed to rank availability: " + err.Error())
			}
			candidates = append(candidates, ranked)
		}
		slots = selectPreferredAvailabilitySlots(candidates)
		if unavailable == 0 && laterDatesCannotImprove(candidates, command, query.Date) {
			searchedThrough = query.Date
			break
		}
	}
	if len(slots) == 0 && unavailable > 0 {
		return plan.incomplete(unavailable), nil
	}
	return s.availabilityResponse(plan, slots, searchedThrough)
}

// ListCommand loads a complete inventory window for conversational selection.
// Patient eligibility and booking policy are identical to Search.
type ListCommand struct {
	StartDate       string `json:"startDate,omitempty"`
	RangeDays       int    `json:"rangeDays,omitempty"`
	Office          string `json:"office"`
	DOB             string `json:"dob,omitempty"`
	Routing         string `json:"routing"`
	PreauthRequired bool   `json:"preauthRequired"`
}

func (s *service) List(ctx context.Context, command ListCommand) (domain.AvailabilityResponse, error) {
	if command.RangeDays != 0 && command.RangeDays != 14 {
		return domain.AvailabilityResponse{}, schedulingError("rangeDays must be 14; use startDate to search a different window")
	}
	plan, err := s.prepareAvailability(ctx, SearchCommand{Office: command.Office, DOB: command.DOB, RequestedDate: command.StartDate, Routing: command.Routing, PreauthRequired: command.PreauthRequired}, 14)
	if err != nil {
		return domain.AvailabilityResponse{}, err
	}
	queries := plan.queries()
	inventory, err := s.readInventory(ctx, queries)
	if err != nil {
		return domain.AvailabilityResponse{}, providerError(err, "Appointment scheduling is temporarily unavailable. Please try again.")
	}
	var slots []domain.AvailabilitySlotOption
	unavailable := 0
	for _, query := range queries {
		daySlots, missing := plan.slots(query, inventory[query.Date])
		unavailable += missing
		slots = append(slots, daySlots...)
	}
	if unavailable > 0 {
		return plan.incomplete(unavailable), nil
	}
	return s.availabilityResponse(plan, slots, plan.end.Format("2006-01-02"))
}

type availabilityPlan struct {
	command                SearchCommand
	requestedDate          string
	start, end, nowEastern time.Time
	office                 *domain.OfficeConfig
	policy                 domain.SchedulingPolicy
	routing                domain.RoutingRule
	columns                []domain.SchedulerColumn
	profiles               map[string]domain.SchedulerProfile
}

func (s *service) prepareAvailability(ctx context.Context, command SearchCommand, days int) (*availabilityPlan, error) {
	var empty *availabilityPlan
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
	if err := domain.ValidateOptionalDOB(command.DOB); err != nil {
		return empty, schedulingError(err.Error())
	}

	if startDate.Format("2006-01-02") <= nowEastern.Format("2006-01-02") {
		return empty, schedulingError("Same-day and past-date appointments are not available. Please search for tomorrow or later.")
	}
	if command.PreauthRequired {
		startDate = enforcePreauthMinDate(startDate, nowEastern)
	}
	maxDate := startDate.AddDate(0, 0, days-1)

	office, err := s.offices.ResolveOffice(command.Office)
	if err != nil {
		return empty, schedulingError(err.Error())
	}
	policy := domain.NewSchedulingPolicy(office)
	routing := policy.SchedulingRouting(domain.ParseRoutingRule(command.Routing), command.DOB)

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
	}

	return &availabilityPlan{command: command, requestedDate: originalRequestedDate, start: startDate, end: maxDate, nowEastern: nowEastern, office: office, policy: policy, routing: routing, columns: allowedColumns, profiles: profileMap}, nil
}

func (p *availabilityPlan) queries() []domain.ScheduleReadQuery {
	var queries []domain.ScheduleReadQuery
	for date := p.start; !date.After(p.end); date = date.AddDate(0, 0, 1) {
		var ids []string
		for _, column := range p.columns {
			if column.WorksOnDay(date.Weekday()) {
				ids = append(ids, column.ID)
			}
		}
		if len(ids) > 0 {
			queries = append(queries, domain.ScheduleReadQuery{ColumnIDs: ids, Date: date.Format("2006-01-02")})
		}
	}
	return queries
}

func (p *availabilityPlan) slots(query domain.ScheduleReadQuery, read domain.ScheduleReadResult) ([]domain.AvailabilitySlotOption, int) {
	date, _ := time.Parse("2006-01-02", query.Date)
	var slots []domain.AvailabilitySlotOption
	missing := 0
	for _, column := range p.columns {
		if !column.WorksOnDay(date.Weekday()) {
			continue
		}
		schedule, ok := read.Columns[column.ID]
		if !ok || !schedule.Complete() {
			missing++
			continue
		}
		displayName := p.office.Columns[column.ID].DisplayName
		if displayName == "" {
			displayName = p.office.ProviderDisplayName(column.ProfileID)
		}
		if displayName == "" {
			displayName = p.profiles[column.ProfileID].Name
		}
		columnID, _ := strconv.Atoi(column.ID)
		profileID, _ := strconv.Atoi(column.ProfileID)
		for _, slot := range availableSlots(p.policy, column, schedule.Appointments, schedule.BlockHolds, date, p.nowEastern) {
			slots = append(slots, domain.AvailabilitySlotOption{Provider: displayName, Time: slot.Time, DateTime: slot.DateTime, ColumnID: columnID, ProfileID: profileID, Duration: column.Interval, SameStartBooked: slot.SameStartBooked, SameStartCapacity: slot.SameStartCapacity, RequiresForce: slot.RequiresForce})
		}
	}
	sortAvailabilitySlots(slots)
	return slots, missing
}

func (p *availabilityPlan) incomplete(missing int) domain.AvailabilityResponse {
	return incompleteResponse(p.requestedDate, p.start.Format("2006-01-02"), p.end.Format("2006-01-02"), missing)
}

func (s *service) availabilityResponse(p *availabilityPlan, slots []domain.AvailabilitySlotOption, searchedThrough string) (domain.AvailabilityResponse, error) {
	if len(p.columns) == 0 {
		return domain.AvailabilityResponse{Status: domain.AvailabilityStatusSuccess, Outcome: domain.AvailabilityOutcomeNoEligibleProviders, RequestedDate: p.requestedDate, NextAction: domain.AvailabilityNextActionAskDifferentPreferences, Message: "No eligible providers found for this office, routing, provider, and DOB.", Slots: []domain.AvailabilitySlotOption{}}, nil
	}
	if len(slots) == 0 {
		return noneResponse(p.requestedDate, p.start.Format("2006-01-02"), p.end.Format("2006-01-02")), nil
	}
	actualDate := slots[0].DateTime[:len("2006-01-02")]
	slots, expires, err := s.signSlots(slots, p.office, p.routing, p.command.DOB, s.now().UTC())
	if err != nil {
		return domain.AvailabilityResponse{}, schedulingError("Failed to create booking tokens: " + err.Error())
	}
	return domain.AvailabilityResponse{Status: domain.AvailabilityStatusSuccess, Outcome: domain.AvailabilityOutcomeFound, AvailabilityFound: true, RequestedDate: p.requestedDate, NextAction: domain.AvailabilityNextActionOfferSlots, ActualDate: actualDate, DateShifted: availabilityDateShifted(p.requestedDate, p.start.Format("2006-01-02"), actualDate), SearchedFrom: p.start.Format("2006-01-02"), SearchedThrough: searchedThrough, BookingTokenExpiresAt: expires.Format(time.RFC3339), Slots: slots}, nil
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
