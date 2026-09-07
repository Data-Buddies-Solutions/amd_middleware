package scheduling

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

const (
	searchForwardDays      = 14
	schedulerSetupCacheTTL = 6 * time.Hour
)

var eastern = domain.EasternLocation()

// SearchCommand is the domain input for one availability search.
type SearchCommand struct {
	RequestedDate   string                      `json:"requestedDate,omitempty"`
	PreferredTime   *AvailabilityTimePreference `json:"preferredTime,omitempty"`
	Provider        string                      `json:"provider"`
	Office          string                      `json:"office"`
	Routing         string                      `json:"routing"`
	DOB             string                      `json:"dob,omitempty"`
	PreauthRequired bool                        `json:"preauthRequired"`
}

type AvailabilityTimeKind string

const (
	AvailabilityTimeMorning   AvailabilityTimeKind = "morning"
	AvailabilityTimeAfternoon AvailabilityTimeKind = "afternoon"
)

// AvailabilityTimePreference is a canonical clinic-local time preference.
type AvailabilityTimePreference struct {
	Kind        AvailabilityTimeKind `json:"kind,omitempty"`
	MinuteOfDay *int                 `json:"minuteOfDay,omitempty"`
}

// Scheduling is the complete scheduling boundary used by HTTP.
type Scheduling interface {
	Search(ctx context.Context, command SearchCommand) (domain.AvailabilityResponse, error)
	List(ctx context.Context, command ListCommand) (domain.AvailabilityResponse, error)
	Book(ctx context.Context, command BookCommand) (BookReceipt, error)
	Cancel(ctx context.Context, command CancelCommand) (CancelReceipt, error)
}

// Category is a stable, provider-independent scheduling outcome.
type Category string

const (
	CategoryValidation               Category = "validation"
	CategoryInvalidBookingToken      Category = "invalid_booking_token"
	CategoryInvalidCancellationToken Category = "invalid_cancellation_token"
	CategoryInvalidRescheduleToken   Category = "invalid_reschedule_token"
	CategoryBookingTokenRequired     Category = "booking_token_required"
	CategoryAppointmentTypeMissing   Category = "appointment_type_unresolved"
	CategoryPatientContextMismatch   Category = "patient_context_mismatch"
	CategorySlotUnavailable          Category = "slot_unavailable"
	CategoryProviderConflict         Category = "provider_conflict"
	CategoryProviderRejected         Category = "provider_rejected"
	CategoryOwnershipMismatch        Category = "ownership_mismatch"
	CategoryWriteFailed              Category = "write_failed"
	CategoryIndeterminateWrite       Category = "indeterminate_write"
)

// Error contains only caller-safe scheduling failure details.
type Error struct {
	category        Category
	providerFailure safeerrors.Category
	message         string
	missing         []string
}

func (e *Error) Error() string {
	return e.message
}

// CategoryOf returns the stable domain category for a Scheduling error.
func CategoryOf(err error) Category {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		return schedulingErr.category
	}
	return CategoryValidation
}

// ProviderFailureOf returns the stable provider category carried by a
// Scheduling error, or none when the failure did not come from AdvancedMD.
func ProviderFailureOf(err error) safeerrors.Category {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		if schedulingErr.providerFailure == "" {
			return safeerrors.CategoryNone
		}
		return schedulingErr.providerFailure
	}
	return safeerrors.CategoryNone
}

// MissingOf returns caller-safe fields required to complete a Scheduling
// command.
func MissingOf(err error) []string {
	var schedulingErr *Error
	if errors.As(err, &schedulingErr) {
		return append([]string(nil), schedulingErr.missing...)
	}
	return nil
}

type service struct {
	records            advancedmd.SchedulingRecords
	bookingTokenSecret string
	appointmentTokens  *AppointmentTokens
	allowRawBooking    bool
	now                func() time.Time

	setupMu        sync.Mutex
	setup          *domain.SchedulerSetup
	setupExpiresAt time.Time
}

// Config makes compatibility behavior explicit at composition time.
type Config struct {
	AllowRawBooking bool
}

// New constructs Scheduling with compatibility behavior disabled.
func New(records advancedmd.SchedulingRecords, bookingTokenSecret string, now func() time.Time) Scheduling {
	return NewWithConfig(records, bookingTokenSecret, now, Config{})
}

// NewWithConfig constructs the single owner for scheduling behavior.
func NewWithConfig(
	records advancedmd.SchedulingRecords,
	bookingTokenSecret string,
	now func() time.Time,
	config Config,
) Scheduling {
	if now == nil {
		now = time.Now
	}
	return &service{
		records:            records,
		bookingTokenSecret: bookingTokenSecret,
		appointmentTokens:  NewAppointmentTokens(bookingTokenSecret, now),
		allowRawBooking:    config.AllowRawBooking,
		now:                now,
	}
}

func (s *service) Search(ctx context.Context, command SearchCommand) (domain.AvailabilityResponse, error) {
	return s.search(ctx, command, 0)
}

// ListCommand loads a complete inventory window for conversational selection.
// Patient eligibility and booking policy are identical to Search.
type ListCommand struct {
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
	if days != 14 && days != 30 && days != 90 {
		return domain.AvailabilityResponse{}, schedulingError("rangeDays must be 14, 30, or 90")
	}
	return s.search(ctx, SearchCommand{Office: command.Office, DOB: command.DOB,
		Routing: command.Routing, PreauthRequired: command.PreauthRequired}, days)
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
	hasPreference := command.RequestedDate != "" || command.PreferredTime != nil
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

type rankedAvailabilitySlot struct {
	slot            domain.AvailabilitySlotOption
	mismatchCount   int
	distanceMinutes int
}

func validatePreferredTime(preferredTime *AvailabilityTimePreference) error {
	if preferredTime == nil {
		return nil
	}
	switch preferredTime.Kind {
	case AvailabilityTimeMorning, AvailabilityTimeAfternoon:
		if preferredTime.MinuteOfDay != nil {
			return errors.New("preferredTime.minuteOfDay is not allowed")
		}
	case "":
		if preferredTime.MinuteOfDay == nil ||
			*preferredTime.MinuteOfDay < 0 ||
			*preferredTime.MinuteOfDay >= 24*60 {
			return errors.New("preferredTime.minuteOfDay must be between 0 and 1439")
		}
	default:
		return errors.New("preferredTime.kind is invalid")
	}
	return nil
}

func hasTwoExactAvailabilityMatches(
	candidates []rankedAvailabilitySlot,
) bool {
	firstSlotKey := ""
	for _, candidate := range candidates {
		if candidate.mismatchCount != 0 || candidate.distanceMinutes != 0 {
			continue
		}
		slotKey := availabilitySlotKey(candidate.slot)
		if firstSlotKey != "" && slotKey != firstSlotKey {
			return true
		}
		firstSlotKey = slotKey
	}
	return false
}

func rankedSlot(
	slot domain.AvailabilitySlotOption,
	requestedDate string,
	preferredTime *AvailabilityTimePreference,
) (rankedAvailabilitySlot, error) {
	start, err := time.Parse("2006-01-02T15:04", slot.DateTime)
	if err != nil {
		return rankedAvailabilitySlot{}, fmt.Errorf("invalid slot datetime %q", slot.DateTime)
	}
	candidate := rankedAvailabilitySlot{slot: slot}
	if requestedDate != "" {
		date, err := time.Parse("2006-01-02", requestedDate)
		if err != nil {
			return rankedAvailabilitySlot{}, fmt.Errorf("invalid requested date %q", requestedDate)
		}
		slotDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		days := absoluteInt(int(slotDate.Sub(date).Hours() / 24))
		if days > 0 {
			candidate.mismatchCount++
			candidate.distanceMinutes += days * 24 * 60
		}
	}
	if preferredTime != nil {
		matches, distance := evaluateTimePreference(start, *preferredTime)
		if !matches {
			candidate.mismatchCount++
		}
		candidate.distanceMinutes += distance
	}
	return candidate, nil
}

func evaluateTimePreference(start time.Time, preference AvailabilityTimePreference) (bool, int) {
	minuteOfDay := start.Hour()*60 + start.Minute()
	switch preference.Kind {
	case AvailabilityTimeMorning:
		if minuteOfDay < 12*60 {
			return true, 0
		}
		return false, minuteOfDay - 12*60 + 1
	case AvailabilityTimeAfternoon:
		if minuteOfDay >= 12*60 {
			return true, 0
		}
		return false, 12*60 - minuteOfDay
	case "":
		distance := absoluteInt(minuteOfDay - *preference.MinuteOfDay)
		return distance == 0, distance
	}
	return false, 24 * 60
}

func selectPreferredAvailabilitySlots(candidates []rankedAvailabilitySlot) []domain.AvailabilitySlotOption {
	sort.SliceStable(candidates, func(i, j int) bool {
		return rankedAvailabilitySlotLess(candidates[i], candidates[j])
	})
	if len(candidates) == 0 {
		return nil
	}
	slots := make([]domain.AvailabilitySlotOption, 0, min(2, len(candidates)))
	selectedSlotKeys := make(map[string]bool, 2)
	for _, candidate := range candidates {
		key := availabilitySlotKey(candidate.slot)
		if selectedSlotKeys[key] {
			continue
		}
		slots = append(slots, candidate.slot)
		selectedSlotKeys[key] = true
		if len(slots) == 2 {
			break
		}
	}
	return slots
}

func availabilitySlotKey(slot domain.AvailabilitySlotOption) string {
	return slot.Provider + "|" + slot.DateTime
}

func selectBroadAvailabilitySlots(slots []domain.AvailabilitySlotOption) []domain.AvailabilitySlotOption {
	if len(slots) <= 2 {
		return slots
	}
	firstIsMorning := slotMinuteOfDay(slots[0]) < 12*60
	for _, slot := range slots[1:] {
		if slotMinuteOfDay(slot) < 12*60 != firstIsMorning {
			return []domain.AvailabilitySlotOption{slots[0], slot}
		}
	}
	return slots[:2]
}

func rankedAvailabilitySlotLess(left, right rankedAvailabilitySlot) bool {
	if left.mismatchCount != right.mismatchCount {
		return left.mismatchCount < right.mismatchCount
	}
	if left.distanceMinutes != right.distanceMinutes {
		return left.distanceMinutes < right.distanceMinutes
	}
	return availabilitySlotLess(left.slot, right.slot)
}

func sortAvailabilitySlots(slots []domain.AvailabilitySlotOption) {
	sort.SliceStable(slots, func(i, j int) bool {
		return availabilitySlotLess(slots[i], slots[j])
	})
}

func availabilitySlotLess(left, right domain.AvailabilitySlotOption) bool {
	if left.DateTime != right.DateTime {
		return left.DateTime < right.DateTime
	}
	if left.Provider != right.Provider {
		return left.Provider < right.Provider
	}
	if left.ColumnID != right.ColumnID {
		return left.ColumnID < right.ColumnID
	}
	return left.ProfileID < right.ProfileID
}

func slotMinuteOfDay(slot domain.AvailabilitySlotOption) int {
	start, _ := time.Parse("2006-01-02T15:04", slot.DateTime)
	return start.Hour()*60 + start.Minute()
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func schedulingError(message string) error {
	return categorizedError(CategoryValidation, message)
}

func categorizedError(category Category, message string) error {
	return &Error{category: category, message: message}
}

func categorizedProviderError(category Category, providerFailure safeerrors.Category, message string) error {
	return &Error{category: category, providerFailure: providerFailure, message: message}
}

func providerError(err error, fallback string) error {
	return &Error{
		category:        CategoryValidation,
		providerFailure: providerCategory(err),
		message:         providerFailureMessage(err, fallback),
	}
}

func providerFailureMessage(err error, fallback string) string {
	switch providerCategory(err) {
	case safeerrors.CategoryAuthentication, safeerrors.CategoryUnavailable:
		return "Service authentication is temporarily unavailable. Please try again."
	default:
		return fallback
	}
}

func providerCategory(err error) safeerrors.Category {
	category := advancedmd.CategoryOf(err)
	if category == safeerrors.CategoryInternal {
		return safeerrors.Classify(err)
	}
	return category
}

func (s *service) schedulerSetup(ctx context.Context, now time.Time) (*domain.SchedulerSetup, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	if s.setup != nil && now.Before(s.setupExpiresAt) {
		return s.setup, nil
	}
	if s.records == nil {
		return nil, fmt.Errorf("AdvancedMD scheduling records are not configured")
	}

	setup, err := s.records.GetSchedulerSetup(ctx)
	if err != nil {
		if s.setup != nil {
			log.Printf("WARNING: scheduler setup refresh failed; using cached setup category=%s", providerCategory(err))
			s.setupExpiresAt = now.Add(time.Minute)
			return s.setup, nil
		}
		return nil, err
	}
	s.setup = &setup
	s.setupExpiresAt = now.Add(schedulerSetupCacheTTL)
	return s.setup, nil
}

func (s *service) signSlots(
	slots []domain.AvailabilitySlotOption,
	office *domain.OfficeConfig,
	routing domain.RoutingRule,
	dob string,
	now time.Time,
) ([]domain.AvailabilitySlotOption, time.Time, error) {
	issuedAt := now.Unix()
	expiresAt := now.Add(slotTokenTTL).Unix()
	appointmentTypeIDs := domain.NewSchedulingPolicy(office).AllowedAppointmentTypeIDs(routing, dob)
	for i := range slots {
		token, err := SignSlotToken(s.bookingTokenSecret, SlotPolicy{
			OfficeID:           office.ID,
			Routing:            string(routing),
			ColumnID:           slots[i].ColumnID,
			ProfileID:          slots[i].ProfileID,
			StartDatetime:      slots[i].DateTime,
			Duration:           slots[i].Duration,
			DOB:                domain.NormalizeDOB(dob),
			AppointmentTypeIDs: appointmentTypeIDs,
			SameStartBooked:    slots[i].SameStartBooked,
			SameStartCapacity:  slots[i].SameStartCapacity,
			RequiresForce:      slots[i].RequiresForce,
			Provider:           slots[i].Provider,
			IssuedAt:           issuedAt,
			ExpiresAt:          expiresAt,
		})
		if err != nil {
			return nil, time.Time{}, err
		}
		slots[i].BookingToken = token
		if slots[i].SameStartBooked == 0 {
			slots[i].SameStartCapacity = 0
		}
	}
	return slots, time.Unix(expiresAt, 0).UTC(), nil
}

func availableSlots(
	policy domain.SchedulingPolicy,
	column domain.SchedulerColumn,
	appointments []domain.Appointment,
	blockHolds []domain.BlockHold,
	date time.Time,
	nowEastern time.Time,
) []domain.AvailableSlot {
	slots := make([]domain.AvailableSlot, 0)
	workStart, workEnd, err := column.ParseWorkHours(date)
	if err != nil {
		return slots
	}

	interval := time.Duration(column.Interval) * time.Minute
	if interval == 0 {
		interval = 15 * time.Minute
	}
	for slotTime := workStart; slotTime.Before(workEnd); slotTime = slotTime.Add(interval) {
		if date.Format("2006-01-02") == nowEastern.Format("2006-01-02") &&
			slotTime.Before(nowEastern.Add(30*time.Minute)) {
			continue
		}
		if domain.IsBlockedByHold(slotTime, interval, blockHolds) ||
			hasDifferentStartOverlap(slotTime, interval, appointments) {
			continue
		}

		sameStartCount := countSameStart(slotTime, appointments)
		sameStart := policy.SameStart(column.ID, slotTime, sameStartCount)
		if !sameStart.Bookable {
			continue
		}
		slot := domain.AvailableSlot{
			Time:              domain.FormatSlotTime(slotTime),
			DateTime:          domain.FormatSlotDateTime(slotTime),
			SameStartBooked:   sameStartCount,
			SameStartCapacity: sameStart.Capacity,
			RequiresForce:     sameStart.RequiresForce,
		}
		slots = append(slots, slot)
	}
	return slots
}

func hasDifferentStartOverlap(slotTime time.Time, duration time.Duration, appointments []domain.Appointment) bool {
	slotEnd := slotTime.Add(duration)
	for _, appointment := range appointments {
		if appointment.StartDateTime.Equal(slotTime) {
			continue
		}
		appointmentEnd := appointment.StartDateTime.Add(time.Duration(appointment.Duration) * time.Minute)
		if slotTime.Before(appointmentEnd) && appointment.StartDateTime.Before(slotEnd) {
			return true
		}
	}
	return false
}

func countSameStart(slotTime time.Time, appointments []domain.Appointment) int {
	count := 0
	for _, appointment := range appointments {
		if appointment.StartDateTime.Equal(slotTime) {
			count++
		}
	}
	return count
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
