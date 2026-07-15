package http

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/auth"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	availabilitySearchForwardDays = 14
	maxAppointmentCommentLength   = 1000
	schedulerSetupCacheTTL        = 6 * time.Hour
)

type schedulingWorkflow struct {
	tokenManager       *auth.TokenManager
	setupClient        *clients.AdvancedMDClient
	appointmentClient  *clients.AdvancedMDRestClient
	bookingTokenSecret string
	allowRawBooking    bool

	schedulerSetupMu        sync.Mutex
	schedulerSetup          *domain.SchedulerSetup
	schedulerSetupExpiresAt time.Time
}

type workflowError struct {
	outcome string
	message string
	missing []string
}

func invalidBookingTokenError() *workflowError {
	return &workflowError{
		outcome: "invalid_booking_token",
		message: "Invalid or expired booking token. Please check availability again and choose a slot.",
	}
}

func newSchedulingWorkflow(tokenManager *auth.TokenManager, setupClient *clients.AdvancedMDClient, appointmentClient *clients.AdvancedMDRestClient, bookingTokenSecret string) *schedulingWorkflow {
	return &schedulingWorkflow{
		tokenManager:       tokenManager,
		setupClient:        setupClient,
		appointmentClient:  appointmentClient,
		bookingTokenSecret: bookingTokenSecret,
	}
}

func (w *schedulingWorkflow) Search(ctx context.Context, req AvailabilityRequest, now time.Time) (domain.AvailabilityResponse, *workflowError) {
	empty := domain.AvailabilityResponse{}
	if req.Date == "" {
		return empty, &workflowError{message: "date is required (YYYY-MM-DD format)"}
	}
	originalRequestedDate := req.Date

	startDate, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		return empty, &workflowError{message: "Invalid date format. Use YYYY-MM-DD."}
	}
	if err := validateOptionalDOB(req.DOB); err != nil {
		return empty, &workflowError{message: err.Error()}
	}

	nowEastern := now.In(eastern)
	if startDate.Format("2006-01-02") <= nowEastern.Format("2006-01-02") {
		return empty, &workflowError{message: "Same-day and past-date appointments are not available. Please search for tomorrow or later."}
	}
	if req.PreauthRequired {
		startDate, req.Date = enforcePreauthMinDate(startDate, nowEastern)
	}
	searchStartDate := startDate.Format("2006-01-02")
	maxDate := startDate.AddDate(0, 0, availabilitySearchForwardDays)
	searchEndDate := maxDate.Format("2006-01-02")

	office, err := resolveOffice(req.Office)
	if err != nil {
		return empty, &workflowError{message: err.Error()}
	}
	policy := domain.NewSchedulingPolicy(office)
	routing := policy.SchedulingRouting(domain.ParseRoutingRule(req.Routing), req.DOB)

	log.Printf("availability: date=%s provider=%q office=%s routing=%q effectiveRouting=%q preauthRequired=%v", req.Date, req.Provider, office.ID, req.Routing, routing, req.PreauthRequired)

	tokenData, err := w.getToken(ctx)
	if err != nil {
		return empty, &workflowError{message: "Failed to get authentication token: " + err.Error()}
	}
	setup, err := w.getSchedulerSetup(ctx, tokenData, now.UTC())
	if err != nil {
		return empty, &workflowError{message: "Failed to get scheduler setup: " + err.Error()}
	}
	if w.appointmentClient == nil {
		return empty, &workflowError{message: "AdvancedMD appointment client is not configured"}
	}

	profileMap := make(map[string]domain.SchedulerProfile, len(setup.Profiles))
	for _, profile := range setup.Profiles {
		profileMap[profile.ID] = profile
	}
	facilityMap := make(map[string]domain.SchedulerFacility, len(setup.Facilities))
	for _, facility := range setup.Facilities {
		facilityMap[facility.ID] = facility
	}

	allowedColumns := policy.EligibleColumns(setup.Columns, profileMap, routing, req.DOB, req.Provider)
	if len(allowedColumns) == 0 {
		if req.Provider != "" {
			return empty, &workflowError{message: fmt.Sprintf("No provider found matching %q. Valid providers: %s", req.Provider, strings.Join(office.ValidProviderNames(), ", "))}
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

	searchDate := startDate
	var providers []domain.ProviderAvailability
	searchIncomplete := false
	unavailableDataChecks := 0

	for !searchDate.After(maxDate) {
		dateStr := searchDate.Format("2006-01-02")
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
			log.Printf("availability: no providers work on %s, skipping", dateStr)
			continue
		}

		var appointmentsByColumn map[string][]domain.Appointment
		var blockHoldsByColumn map[string][]domain.BlockHold
		var fetch sync.WaitGroup
		fetch.Add(2)
		go func() {
			defer fetch.Done()
			appointmentsByColumn = w.appointmentClient.GetAppointmentsForColumns(ctx, tokenData, workingColumnIDs, dateStr)
		}()
		go func() {
			defer fetch.Done()
			blockHoldsByColumn = w.appointmentClient.GetBlockHoldsForColumns(ctx, tokenData, workingColumnIDs, dateStr)
		}()
		fetch.Wait()

		providers = nil
		for _, column := range allowedColumns {
			if !workingColumnSet[column.ID] {
				continue
			}
			if _, ok := appointmentsByColumn[column.ID]; !ok {
				searchIncomplete = true
				unavailableDataChecks++
				log.Printf("availability: skipping column %s — appointment data unavailable", column.ID)
				continue
			}

			profile := profileMap[column.ProfileID]
			facility := facilityMap[column.FacilityID]
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

			allSlots := calculateAvailableSlots(policy, column, appointmentsByColumn[column.ID], blockHoldsByColumn[column.ID], searchDate, nowEastern)
			columnID, _ := strconv.Atoi(column.ID)
			profileID, _ := strconv.Atoi(column.ProfileID)
			provider := domain.ProviderAvailability{
				Name:           displayName,
				ColumnID:       columnID,
				ProfileID:      profileID,
				Facility:       facility.Name,
				SlotDuration:   column.Interval,
				TotalAvailable: len(allSlots),
				Slots:          []domain.AvailableSlot{},
			}
			if len(allSlots) > 0 {
				provider.FirstAvailable = allSlots[0].Time
				provider.LastAvailable = allSlots[len(allSlots)-1].Time
				provider.Slots = selectBalancedDisplaySlots(allSlots, 5)
			}
			providers = append(providers, provider)
		}

		if providersHaveAvailability(providers) {
			break
		}
		searchDate = searchDate.AddDate(0, 0, 1)
		log.Printf("availability: no slots on %s, searching forward to %s", dateStr, searchDate.Format("2006-01-02"))
	}

	if !providersHaveAvailability(providers) {
		if searchIncomplete {
			return domain.AvailabilityResponse{
				Status:                domain.AvailabilityStatusError,
				Outcome:               domain.AvailabilityOutcomeSearchIncomplete,
				AvailabilityFound:     false,
				RequestedDate:         originalRequestedDate,
				ShouldRetrySameSearch: true,
				NextAction:            domain.AvailabilityNextActionRetryOnceThenAskPreferences,
				SearchedFrom:          searchStartDate,
				SearchedThrough:       searchEndDate,
				Message:               incompleteAvailabilityMessage(searchStartDate, searchEndDate, unavailableDataChecks),
				Slots:                 []domain.AvailabilitySlotOption{},
			}, nil
		}
		return domain.AvailabilityResponse{
			Status:                domain.AvailabilityStatusSuccess,
			Outcome:               domain.AvailabilityOutcomeNoAvailability,
			AvailabilityFound:     false,
			RequestedDate:         originalRequestedDate,
			ShouldRetrySameSearch: false,
			NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
			SearchedFrom:          searchStartDate,
			SearchedThrough:       searchEndDate,
			Message:               noAvailabilityMessage(searchStartDate, searchEndDate),
			Slots:                 []domain.AvailabilitySlotOption{},
		}, nil
	}

	actualDate := searchDate.Format("2006-01-02")
	slots, err := w.addBookingTokens(flattenAvailabilitySlots(providers), office, routing, req.DOB, now.UTC())
	if err != nil {
		return empty, &workflowError{message: "Failed to create booking tokens: " + err.Error()}
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
		SearchedThrough:       actualDate,
		Slots:                 slots,
	}, nil
}

func (w *schedulingWorkflow) Book(ctx context.Context, req BookAppointmentRequest, now time.Time) (BookAppointmentResponse, *workflowError) {
	var office *domain.OfficeConfig
	if req.Office != "" || req.BookingToken == "" {
		var err error
		office, err = resolveOffice(req.Office)
		if err != nil {
			return BookAppointmentResponse{}, &workflowError{message: err.Error()}
		}
	}
	if req.BookingToken != "" {
		tokenOffice, err := w.applyBookingToken(&req, office, now.UTC())
		if err != nil {
			return BookAppointmentResponse{}, invalidBookingTokenError()
		}
		office = tokenOffice
	}

	log.Printf("book-appointment: request office=%s routing=%q bookingToken=%t legacyRaw=%t typeId=%d", office.ID, req.Routing, req.BookingToken != "", req.BookingToken == "", req.AppointmentTypeID)

	if req.PatientID == "" {
		return BookAppointmentResponse{}, &workflowError{message: "patientId is required"}
	}
	if req.ColumnID == 0 {
		return BookAppointmentResponse{}, &workflowError{message: "columnId is required"}
	}
	if req.ProfileID == 0 {
		return BookAppointmentResponse{}, &workflowError{message: "profileId is required"}
	}
	if req.StartDatetime == "" {
		return BookAppointmentResponse{}, &workflowError{message: "startDatetime is required"}
	}
	if req.Duration == 0 {
		return BookAppointmentResponse{}, &workflowError{message: "duration is required"}
	}
	if err := validateOptionalDOB(req.DOB); err != nil {
		return BookAppointmentResponse{}, &workflowError{message: err.Error()}
	}
	appointmentComment := buildBookingAppointmentComment(req.AppointmentReason, req.ReferringDoctor)
	if len([]rune(appointmentComment)) > maxAppointmentCommentLength {
		return BookAppointmentResponse{}, &workflowError{message: fmt.Sprintf("appointment comments must be %d characters or fewer", maxAppointmentCommentLength)}
	}

	policy := domain.NewSchedulingPolicy(office)
	decision, policyErr := policy.PrepareBooking(domain.BookingPolicyRequest{
		ColumnID:          req.ColumnID,
		ProfileID:         req.ProfileID,
		AppointmentTypeID: req.AppointmentTypeID,
		Routing:           domain.ParseRoutingRule(req.Routing),
		DOB:               req.DOB,
		Intent: domain.AppointmentIntent{
			VisitCategory: req.VisitCategory,
			VisitKind:     req.VisitKind,
			PatientStatus: req.PatientStatus,
			AgeBand:       req.AgeBand,
			DOB:           req.DOB,
			IsPostOp:      req.IsPostOp,
			VisitReason:   req.VisitReason,
		},
	})
	if policyErr != nil {
		return BookAppointmentResponse{}, &workflowError{outcome: policyErr.Outcome, message: policyErr.Message, missing: policyErr.Missing}
	}
	if len(req.bookingAppointmentTypeIDs) > 0 && !slices.Contains(req.bookingAppointmentTypeIDs, decision.AppointmentTypeID) {
		return BookAppointmentResponse{}, invalidBookingTokenError()
	}
	req.Routing = string(decision.Routing)
	req.AppointmentTypeID = decision.AppointmentTypeID

	patientID, err := strconv.Atoi(req.PatientID)
	if err != nil {
		return BookAppointmentResponse{}, &workflowError{message: "patientId must be numeric"}
	}
	if req.BookingToken == "" && !w.allowRawBooking {
		return BookAppointmentResponse{}, &workflowError{
			outcome: "booking_token_required",
			message: "bookingToken is required. Please check availability again and choose one of the returned slots.",
		}
	}

	tokenData, err := w.getToken(ctx)
	if err != nil {
		return BookAppointmentResponse{}, &workflowError{message: "Failed to get authentication token: " + err.Error()}
	}
	if w.appointmentClient == nil {
		return BookAppointmentResponse{}, &workflowError{message: "Failed to book appointment: AdvancedMD appointment client is not configured"}
	}

	facilityID, _ := strconv.Atoi(office.FacilityID)
	force := 0
	if req.bookingRequiresForce {
		force = 1
	}
	appointmentID, err := w.appointmentClient.BookAppointment(ctx, tokenData, clients.BookAppointmentParams{
		PatientID:     patientID,
		ColumnID:      req.ColumnID,
		ProfileID:     req.ProfileID,
		StartDatetime: req.StartDatetime,
		Duration:      req.Duration,
		AppointmentType: []struct {
			ID int `json:"id"`
		}{{ID: decision.EnvironmentTypeID}},
		EpisodeID:  1,
		FacilityID: facilityID,
		Color:      decision.Color,
		Force:      force,
		Comments:   appointmentComment,
	})
	if err != nil {
		log.Printf("book-appointment: AMD error: %v", err)
		if strings.Contains(err.Error(), "conflict") {
			return BookAppointmentResponse{}, &workflowError{
				outcome: "slot_unavailable",
				message: "This time slot is no longer available. Please check availability again and choose a different slot.",
			}
		}
		return BookAppointmentResponse{}, &workflowError{message: "Failed to book appointment: " + err.Error()}
	}

	log.Printf("book-appointment: success office=%s", office.ID)
	return buildBookAppointmentReceipt(req, office, appointmentID), nil
}

func (w *schedulingWorkflow) getToken(ctx context.Context) (*domain.TokenData, error) {
	if w.tokenManager == nil {
		return nil, fmt.Errorf("token manager is not configured")
	}
	return w.tokenManager.GetToken(ctx)
}

func (w *schedulingWorkflow) getSchedulerSetup(ctx context.Context, tokenData *domain.TokenData, now time.Time) (*domain.SchedulerSetup, error) {
	w.schedulerSetupMu.Lock()
	defer w.schedulerSetupMu.Unlock()

	if w.schedulerSetup != nil && now.Before(w.schedulerSetupExpiresAt) {
		return w.schedulerSetup, nil
	}
	var (
		setup *domain.SchedulerSetup
		err   error
	)
	if w.setupClient == nil {
		err = fmt.Errorf("scheduler setup client is not configured")
	} else {
		setup, err = w.setupClient.GetSchedulerSetup(ctx, tokenData)
	}
	if err != nil {
		if w.schedulerSetup != nil {
			log.Printf("WARNING: scheduler setup refresh failed; using cached setup: %v", err)
			w.schedulerSetupExpiresAt = now.Add(time.Minute)
			return w.schedulerSetup, nil
		}
		return nil, err
	}
	w.schedulerSetup = setup
	w.schedulerSetupExpiresAt = now.Add(schedulerSetupCacheTTL)
	return setup, nil
}

func providersHaveAvailability(providers []domain.ProviderAvailability) bool {
	for _, provider := range providers {
		if provider.TotalAvailable > 0 {
			return true
		}
	}
	return false
}

func buildBookAppointmentReceipt(req BookAppointmentRequest, office *domain.OfficeConfig, appointmentID int) BookAppointmentResponse {
	colIDStr := strconv.Itoa(req.ColumnID)
	providerName := ""
	if col, ok := office.Columns[colIDStr]; ok {
		providerName = col.DisplayName
	}
	if providerName == "" {
		providerName = office.ProviderDisplayName(strconv.Itoa(req.ProfileID))
	}
	appointmentTypeName, _ := office.AppointmentTypeName(req.AppointmentTypeID)

	return BookAppointmentResponse{
		Status:              "booked",
		AppointmentID:       appointmentID,
		PatientID:           req.PatientID,
		PatientName:         normalizeBookingPatientName(req.PatientName),
		ProviderName:        providerName,
		LocationName:        office.DisplayName,
		StartDatetime:       req.StartDatetime,
		Duration:            req.Duration,
		AppointmentTypeID:   req.AppointmentTypeID,
		AppointmentTypeName: appointmentTypeName,
		Message:             "Appointment booked successfully",
	}
}

func normalizeBookingPatientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	if parts := strings.SplitN(name, ",", 2); len(parts) == 2 {
		first := strings.TrimSpace(parts[1])
		last := strings.TrimSpace(parts[0])
		name = strings.TrimSpace(strings.Join([]string{first, last}, " "))
	}

	if name == strings.ToUpper(name) || name == strings.ToLower(name) {
		return cases.Title(language.English).String(strings.ToLower(name))
	}
	return name
}

func buildBookingAppointmentComment(appointmentReason string, referringDoctor string) string {
	appointmentReason = normalizeAppointmentCommentPart(appointmentReason)
	referringDoctor = normalizeAppointmentCommentPart(referringDoctor)
	if appointmentReason == "" && referringDoctor == "" {
		return ""
	}
	if appointmentReason == "" {
		appointmentReason = "none"
	}
	if referringDoctor == "" {
		referringDoctor = "none"
	}

	lines := []string{
		"Appointment reason: " + appointmentReason,
		"Referring doctor: " + referringDoctor,
		"- AI",
	}

	return strings.Join(lines, "\n")
}

func normalizeAppointmentCommentPart(value string) string {
	return strings.TrimSpace(value)
}

func availabilityDateShifted(requestedDate, searchStartDate, actualDate string) bool {
	if actualDate != "" {
		return actualDate != requestedDate
	}
	return searchStartDate != requestedDate
}

func noAvailabilityMessage(searchStartDate, searchEndDate string) string {
	return fmt.Sprintf("No availability was found from %s through %s. Do not search this same window again unless the patient changes date, provider, office, or appointment type.", searchStartDate, searchEndDate)
}

func incompleteAvailabilityMessage(searchStartDate, searchEndDate string, unavailableDataChecks int) string {
	return fmt.Sprintf("Availability could not be fully checked from %s through %s because appointment data was unavailable for %d provider-date checks. Retry once; if it still cannot be checked, ask for different preferences.", searchStartDate, searchEndDate, unavailableDataChecks)
}

func flattenAvailabilitySlots(providers []domain.ProviderAvailability) []domain.AvailabilitySlotOption {
	var slots []domain.AvailabilitySlotOption
	for _, provider := range providers {
		if provider.TotalAvailable == 0 {
			continue
		}
		for _, slot := range provider.Slots {
			slots = append(slots, domain.AvailabilitySlotOption{
				Provider:          provider.Name,
				Time:              slot.Time,
				DateTime:          slot.DateTime,
				ColumnID:          provider.ColumnID,
				ProfileID:         provider.ProfileID,
				Duration:          provider.SlotDuration,
				SameStartBooked:   slot.SameStartBooked,
				SameStartCapacity: slot.SameStartCapacity,
				RequiresForce:     slot.RequiresForce,
			})
		}
	}
	return slots
}

func selectBalancedDisplaySlots(slots []domain.AvailableSlot, limit int) []domain.AvailableSlot {
	if limit <= 0 || len(slots) == 0 {
		return []domain.AvailableSlot{}
	}
	if len(slots) <= limit {
		return slots
	}
	if limit == 1 {
		return slots[:1]
	}

	selected := make([]domain.AvailableSlot, 0, limit)
	seenIndexes := make(map[int]bool, limit)
	lastIndex := len(slots) - 1
	for i := 0; i < limit; i++ {
		// Evenly sample the chronological slot list, rounded to the nearest
		// index. This keeps first/latest options while surfacing middle-day
		// choices that first-N truncation would hide.
		index := (i*lastIndex + (limit-1)/2) / (limit - 1)
		if seenIndexes[index] {
			continue
		}
		seenIndexes[index] = true
		selected = append(selected, slots[index])
	}

	return selected
}

// calculateAvailableSlots generates available time slots for a column on a single day.
// nowEastern is used to filter out past slots when the date is today.
func calculateAvailableSlots(policy domain.SchedulingPolicy, col domain.SchedulerColumn, appointments []domain.Appointment, blockHolds []domain.BlockHold, date time.Time, nowEastern time.Time) []domain.AvailableSlot {
	var slots []domain.AvailableSlot

	// Skip if provider doesn't work this day
	if !col.WorksOnDay(date.Weekday()) {
		return slots
	}

	// Get work hours
	workStart, workEnd, err := col.ParseWorkHours(date)
	if err != nil {
		return slots
	}

	// Determine cutoff for past slots: if date is today, skip slots before now + 30 min
	today := nowEastern.Format("2006-01-02")
	isToday := date.Format("2006-01-02") == today
	cutoff := nowEastern.Add(30 * time.Minute)

	interval := time.Duration(col.Interval) * time.Minute
	if interval == 0 {
		interval = 15 * time.Minute
	}

	for slotTime := workStart; slotTime.Before(workEnd); slotTime = slotTime.Add(interval) {
		// Filter past slots
		if isToday {
			slotInEastern := time.Date(slotTime.Year(), slotTime.Month(), slotTime.Day(),
				slotTime.Hour(), slotTime.Minute(), 0, 0, nowEastern.Location())
			if slotInEastern.Before(cutoff) {
				continue
			}
		}

		if domain.IsBlockedByHold(slotTime, interval, blockHolds) {
			continue
		}

		// AMD 4101: Block if any different-start appointment overlaps this slot's full booking range.
		if hasDifferentStartOverlappingAppointment(slotTime, interval, appointments) {
			continue
		}

		// AMD 4186: Check same-start-time appointment count against per-column capacity.
		sameStartCount := countSameStartAppointments(slotTime, appointments)
		sameStart := policy.SameStart(col.ID, slotTime, sameStartCount)
		if !sameStart.Bookable {
			continue
		}

		slot := domain.AvailableSlot{
			Time:     domain.FormatSlotTime(slotTime),
			DateTime: domain.FormatSlotDateTime(slotTime),
		}
		if sameStartCount > 0 {
			slot.SameStartBooked = sameStartCount
			slot.SameStartCapacity = sameStart.Capacity
			slot.RequiresForce = sameStart.RequiresForce
		}
		slots = append(slots, slot)
	}

	return slots
}

// hasDifferentStartOverlappingAppointment checks if a different-start appointment
// overlaps the full booking range [slotTime, slotTime+slotDuration). Same-start
// appointments are handled separately as per-column capacity because AMD's 4186
// rule is distinct from 4101 duration-overlap blocking.
func hasDifferentStartOverlappingAppointment(slotTime time.Time, slotDuration time.Duration, appointments []domain.Appointment) bool {
	slotEnd := slotTime.Add(slotDuration)
	for _, appt := range appointments {
		if appt.StartDateTime.Equal(slotTime) {
			continue
		}
		apptEnd := appt.StartDateTime.Add(time.Duration(appt.Duration) * time.Minute)
		// Two intervals overlap when each starts before the other ends
		if slotTime.Before(apptEnd) && appt.StartDateTime.Before(slotEnd) {
			return true
		}
	}
	return false
}

// countSameStartAppointments counts appointments that start at exactly the given slot time.
// AMD returns error 4186 when this count exceeds maxApptsPerSlot.
func countSameStartAppointments(slotTime time.Time, appointments []domain.Appointment) int {
	count := 0
	for _, appt := range appointments {
		if appt.StartDateTime.Equal(slotTime) {
			count++
		}
	}
	return count
}

// enforcePreauthMinDate advances the requested date to 14 days from now if it's too soon.
// Returns the (possibly advanced) date and its YYYY-MM-DD string.
func enforcePreauthMinDate(requestedDate time.Time, now time.Time) (time.Time, string) {
	// Truncate to date-only (midnight) so time-of-day doesn't affect the comparison
	minDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 14)
	if requestedDate.Before(minDate) {
		log.Printf("availability: preauth required — auto-advanced to %s (14-day minimum)", minDate.Format("2006-01-02"))
		return minDate, minDate.Format("2006-01-02")
	}
	return requestedDate, requestedDate.Format("2006-01-02")
}
