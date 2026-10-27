package advancedmd

import (
	"context"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/session"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var eastern = domain.EasternLocation()

const scheduledVisitStatus = "0"

type Adapter struct {
	session    session.Session
	xmlClient  *clients.AdvancedMDClient
	restClient *clients.AdvancedMDRestClient
	now        func() time.Time
}

func NewAdapter(
	amdSession session.Session,
	xmlClient *clients.AdvancedMDClient,
	restClient *clients.AdvancedMDRestClient,
	now func() time.Time,
) *Adapter {
	return &Adapter{
		session:    amdSession,
		xmlClient:  xmlClient,
		restClient: restClient,
		now:        now,
	}
}

func (a *Adapter) SearchPatients(ctx context.Context, search domain.PatientSearch) (domain.PatientCandidateRead, error) {
	token, err := a.token(ctx)
	if err != nil {
		return domain.PatientCandidateRead{}, err
	}
	var read domain.PatientCandidateRead
	if search.Phone != "" {
		read, err = a.xmlClient.LookupPatientByPhone(ctx, token, search.Phone)
	} else {
		read, err = a.xmlClient.LookupPatient(ctx, token, search.LastName, search.FirstName)
	}
	if err != nil {
		return domain.PatientCandidateRead{}, a.classify(token, err)
	}
	return read, nil
}

func (a *Adapter) ReadPatientCandidates(ctx context.Context, firstName, dob string) (domain.PatientCandidateRead, error) {
	token, err := a.token(ctx)
	if err != nil {
		return domain.PatientCandidateRead{}, err
	}
	read, err := a.xmlClient.LookupPatientCandidates(ctx, token, firstName, dob)
	if err != nil {
		return domain.PatientCandidateRead{}, a.classify(token, err)
	}
	return read, nil
}

func (a *Adapter) GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error) {
	token, err := a.token(ctx)
	if err != nil {
		return domain.PatientDemographics{}, err
	}
	demographics, err := a.xmlClient.GetDemographic(ctx, token, patientID)
	if err != nil {
		return domain.PatientDemographics{}, a.classify(token, err)
	}
	return demographics, nil
}

func (a *Adapter) CreatePatient(ctx context.Context, command domain.PatientCreate) (domain.CreatedPatient, error) {
	token, err := a.token(ctx)
	if err != nil {
		return domain.CreatedPatient{}, err
	}
	office, ok := domain.LookupOfficeByID(command.OfficeID)
	if !ok {
		return domain.CreatedPatient{}, NewError(safeerrors.CategoryInternal)
	}

	created, err := a.xmlClient.AddPatient(ctx, token, command, office.DefaultProfileID)
	if err != nil {
		return domain.CreatedPatient{}, a.classifyMutation(token, err)
	}
	return created, nil
}

func (a *Adapter) AddPatientInsurance(ctx context.Context, command domain.PatientInsurance) error {
	token, err := a.token(ctx)
	if err != nil {
		return err
	}
	if err := a.xmlClient.AddInsurance(
		ctx,
		token,
		command.PatientID,
		command.RespPartyID,
		command.CarrierID,
		command.SubscriberNum,
	); err != nil {
		return a.classifyMutation(token, err)
	}
	return nil
}

func (a *Adapter) EndDatePatientInsurance(ctx context.Context, command domain.PatientInsuranceEnd) error {
	token, err := a.token(ctx)
	if err != nil {
		return err
	}
	if err := a.xmlClient.EndDateInsurance(ctx, token, command.PatientID, command.InsPlanID); err != nil {
		return a.classifyMutation(token, err)
	}
	return nil
}

func (a *Adapter) ReadPatientAppointmentsForDay(
	ctx context.Context,
	query AppointmentDayQuery,
) (AppointmentRead, error) {
	token, err := a.token(ctx)
	if err != nil {
		return AppointmentRead{}, err
	}
	if query.Day.IsZero() {
		return AppointmentRead{}, NewError(safeerrors.CategoryInternal)
	}
	patientID, err := strconv.Atoi(query.PatientID)
	if err != nil {
		return AppointmentRead{}, NewError(safeerrors.CategoryInternal)
	}

	read := AppointmentRead{
		Appointments: make([]domain.PatientAppointment, 0),
		Complete:     true,
	}
	for _, officeID := range query.OfficeIDs {
		office, ok := domain.LookupOfficeByID(officeID)
		if !ok {
			return read, NewError(safeerrors.CategoryInternal)
		}
		officeRead, err := a.patientAppointmentsForOfficeDay(
			ctx,
			token,
			patientID,
			office,
			query.Day,
		)
		read.ProviderReads += officeRead.ProviderReads
		if err != nil {
			return read, err
		}
		read.Appointments = append(read.Appointments, officeRead.Appointments...)
		read.Complete = read.Complete && officeRead.Complete
	}
	return read, nil
}

func (a *Adapter) ReadPatientAppointments(ctx context.Context, query domain.PatientAppointmentsQuery) (AppointmentRead, error) {
	token, err := a.token(ctx)
	if err != nil {
		return AppointmentRead{}, err
	}
	patientIDNumber, err := strconv.Atoi(query.PatientID)
	if err != nil {
		return AppointmentRead{}, NewError(safeerrors.CategoryInternal)
	}

	lookup, err := newAppointmentLookup(query.OfficeIDs)
	if err != nil {
		return AppointmentRead{}, err
	}
	now := a.now().In(eastern)
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.UTC)

	visits, err := a.xmlClient.GetPatientVisits(ctx, token, query.PatientID)
	if err != nil {
		return AppointmentRead{ProviderReads: 1}, a.classify(token, err)
	}
	var scheduled []clients.PatientVisit
	var days []string
	seenDays := make(map[string]bool)
	for _, visit := range visits {
		if visit.Status != scheduledVisitStatus || !visit.Start.After(cutoff) {
			continue
		}
		scheduled = append(scheduled, visit)
		day := appointmentDay(visit.Start)
		if !seenDays[day] {
			seenDays[day] = true
			days = append(days, day)
		}
	}
	providerReads := 1 + len(days)

	type dayResult struct {
		appointments []clients.AMDAppointmentResponse
		err          error
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan dayResult, len(days))
	columnIDs := strings.Join(lookup.columnIDs, "-")
	for _, day := range days {
		go func() {
			appointments, err := a.restClient.GetAppointmentsByDay(ctx, token, columnIDs, day)
			results <- dayResult{appointments: appointments, err: err}
		}()
	}

	var rawAppointments []clients.AMDAppointmentResponse
	var firstErr error
	for range days {
		result := <-results
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			cancel()
		}
		rawAppointments = append(rawAppointments, result.appointments...)
	}
	if firstErr != nil {
		return AppointmentRead{ProviderReads: providerReads}, a.classify(token, firstErr)
	}

	read := patientAppointmentRead(rawAppointments, patientIDNumber, lookup.officeByColumn, nil, &cutoff)
	appendUnmappedVisits(&read, scheduled)
	normalizePatientAppointments(&read)
	read.ProviderReads = providerReads
	return read, nil
}

func appendUnmappedVisits(read *AppointmentRead, scheduled []clients.PatientVisit) {
	mapped := make(map[int]bool, len(read.Appointments))
	for _, appointment := range read.Appointments {
		mapped[appointment.ID] = true
	}
	for _, visit := range scheduled {
		if mapped[visit.ID] {
			continue
		}
		read.Appointments = append(read.Appointments, domain.PatientAppointment{
			ID:       visit.ID,
			Start:    visit.Start,
			Provider: visitProviderName(visit.Provider),
			Type:     titleCase(visit.Type),
			Facility: titleCase(visit.Facility),
		})
	}
}

func (a *Adapter) patientAppointmentsForOfficeDay(
	ctx context.Context,
	token *session.TokenData,
	patientID int,
	office *domain.OfficeConfig,
	day time.Time,
) (AppointmentRead, error) {
	if office == nil || day.IsZero() {
		return AppointmentRead{}, NewError(safeerrors.CategoryInternal)
	}
	rawAppointments, err := a.restClient.GetAppointmentsByDay(
		ctx,
		token,
		strings.Join(office.AllowedColumnIDs(), "-"),
		appointmentDay(day),
	)
	if err != nil {
		return AppointmentRead{ProviderReads: 1}, a.classify(token, err)
	}
	read := patientAppointmentRead(rawAppointments, patientID, nil, office, nil)
	read.ProviderReads = 1
	return read, nil
}

type appointmentLookup struct {
	columnIDs      []string
	officeByColumn map[int]*domain.OfficeConfig
}

func newAppointmentLookup(officeIDs []string) (appointmentLookup, error) {
	lookup := appointmentLookup{
		columnIDs:      make([]string, 0),
		officeByColumn: make(map[int]*domain.OfficeConfig),
	}
	seenOffices := make(map[string]bool, len(officeIDs))
	for _, officeID := range officeIDs {
		if seenOffices[officeID] {
			continue
		}
		seenOffices[officeID] = true

		office, ok := domain.LookupOfficeByID(officeID)
		if !ok {
			return appointmentLookup{}, NewError(safeerrors.CategoryInternal)
		}
		for _, columnID := range office.AllowedColumnIDs() {
			columnIDNumber, err := strconv.Atoi(columnID)
			if err != nil {
				return appointmentLookup{}, NewError(safeerrors.CategoryInternal)
			}
			if _, exists := lookup.officeByColumn[columnIDNumber]; exists {
				continue
			}
			lookup.officeByColumn[columnIDNumber] = office
			lookup.columnIDs = append(lookup.columnIDs, columnID)
		}
	}
	if len(lookup.columnIDs) == 0 {
		return appointmentLookup{}, NewError(safeerrors.CategoryInternal)
	}
	sort.Strings(lookup.columnIDs)
	return lookup, nil
}

func patientAppointmentRead(
	rawAppointments []clients.AMDAppointmentResponse,
	patientID int,
	officeByColumn map[int]*domain.OfficeConfig,
	fallbackOffice *domain.OfficeConfig,
	cutoff *time.Time,
) AppointmentRead {
	read := AppointmentRead{
		Appointments: make([]domain.PatientAppointment, 0),
		Complete:     true,
	}
	for _, raw := range rawAppointments {
		if raw.PatientID <= 0 {
			read.Complete = false
			continue
		}
		if raw.PatientID != patientID {
			continue
		}
		if raw.ID <= 0 {
			read.Complete = false
			continue
		}
		start, err := clients.ParseDateTime(raw.StartDateTime)
		if err != nil {
			read.Complete = false
			continue
		}
		if cutoff != nil && !start.After(*cutoff) {
			continue
		}
		office := officeByColumn[raw.ColumnID]
		if office == nil {
			office = fallbackOffice
		}
		if office == nil {
			read.Complete = false
			continue
		}

		typeID := 0
		typeName := ""
		if len(raw.AppointmentTypes) > 0 {
			providerTypeID := raw.AppointmentTypes[0]
			canonicalID, ok := domain.CanonicalAppointmentTypeID(providerTypeID)
			if !ok && providerTypeID <= 0 {
				read.Complete = false
			} else if ok {
				typeID = canonicalID
				var named bool
				typeName, named = office.AppointmentTypeName(typeID)
				if !named {
					read.Complete = false
				}
			} else {
				typeID = providerTypeID
				typeName = "Appointment"
			}
		} else {
			read.Complete = false
		}
		provider := office.FriendlyProviderName(raw.Provider)
		if strings.TrimSpace(raw.Provider) == "" || provider == "" {
			read.Complete = false
		}
		facility := titleCase(raw.Facility)
		if facility == "" {
			facility = office.DisplayName
		}

		read.Appointments = append(read.Appointments, domain.PatientAppointment{
			ID:                raw.ID,
			Start:             start,
			Provider:          provider,
			Type:              typeName,
			AppointmentTypeID: typeID,
			Facility:          facility,
			OfficeID:          office.ID,
			Office:            office.DisplayName,
		})
	}
	return read
}

func normalizePatientAppointments(read *AppointmentRead) {
	sort.Slice(read.Appointments, func(i, j int) bool {
		left := read.Appointments[i]
		right := read.Appointments[j]
		if !left.Start.Equal(right.Start) {
			return left.Start.Before(right.Start)
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.OfficeID != right.OfficeID {
			return left.OfficeID < right.OfficeID
		}
		return left.Provider < right.Provider
	})
	deduplicated := read.Appointments[:0]
	seenAppointmentIDs := make(map[int]bool, len(read.Appointments))
	for _, appointment := range read.Appointments {
		if seenAppointmentIDs[appointment.ID] {
			continue
		}
		seenAppointmentIDs[appointment.ID] = true
		deduplicated = append(deduplicated, appointment)
	}
	read.Appointments = deduplicated
}

func (a *Adapter) ReadAppointmentState(
	ctx context.Context,
	query AppointmentStateQuery,
) (AppointmentState, error) {
	token, err := a.token(ctx)
	if err != nil {
		return AppointmentState{}, err
	}
	if query.AppointmentID <= 0 || query.Start.IsZero() {
		return AppointmentState{}, NewError(safeerrors.CategoryInternal)
	}
	office, ok := domain.LookupOfficeByID(query.OfficeID)
	if !ok {
		return AppointmentState{}, NewError(safeerrors.CategoryInternal)
	}

	rawAppointments, err := a.restClient.GetAppointmentsByDay(
		ctx,
		token,
		strings.Join(office.AllowedColumnIDs(), "-"),
		appointmentDay(query.Start),
	)
	if err != nil {
		return AppointmentState{}, a.classify(token, err)
	}

	state := AppointmentState{Complete: true}
	for _, raw := range rawAppointments {
		if raw.ID == query.AppointmentID {
			state.Exists = true
			return state, nil
		}
		if raw.ID <= 0 {
			state.Complete = false
		}
	}
	return state, nil
}

func appointmentDay(value time.Time) string {
	return value.Format("2006-01-02")
}

func (a *Adapter) GetSchedulerSetup(ctx context.Context) (domain.SchedulerSetup, error) {
	token, err := a.token(ctx)
	if err != nil {
		return domain.SchedulerSetup{}, err
	}
	setup, err := a.xmlClient.GetSchedulerSetup(ctx, token)
	if err != nil {
		return domain.SchedulerSetup{}, a.classify(token, err)
	}
	if setup == nil {
		return domain.SchedulerSetup{}, NewError(safeerrors.CategoryInvalidResponse)
	}
	return *setup, nil
}

func (a *Adapter) ReadColumnDay(ctx context.Context, columnID, date string) (domain.ColumnSchedule, error) {
	token, err := a.token(ctx)
	if err != nil {
		return domain.ColumnSchedule{}, err
	}
	var appointments []domain.Appointment
	var holds []domain.BlockHold
	var appointmentsErr, holdsErr error
	var reads sync.WaitGroup
	reads.Go(func() {
		appointments, appointmentsErr = a.restClient.GetAppointments(ctx, token, columnID, date)
	})
	reads.Go(func() {
		holds, holdsErr = a.restClient.GetBlockHolds(ctx, token, columnID, date)
	})
	reads.Wait()
	var firstErr error
	for _, err := range []error{appointmentsErr, holdsErr} {
		if err != nil {
			failure := a.classify(token, err)
			if firstErr == nil {
				firstErr = failure
			}
			log.Printf("schedule read incomplete category=%s", CategoryOf(failure))
		}
	}
	if firstErr != nil {
		return domain.ColumnSchedule{}, firstErr
	}
	return domain.ColumnSchedule{
		Appointments:         distinctRows(appointments, appointmentKey),
		BlockHolds:           distinctRows(holds, holdKey),
		AppointmentsComplete: true,
		BlockHoldsComplete:   true,
	}, nil
}

func (a *Adapter) ReadScheduleRange(ctx context.Context, query domain.ScheduleRangeQuery) (map[string]domain.ScheduleReadResult, error) {
	token, err := a.token(ctx)
	if err != nil {
		return nil, err
	}
	columnIDs := strings.Join(query.ColumnIDs, "-")
	var months []string
	for month := time.Date(query.Start.Year(), query.Start.Month(), 1, 0, 0, 0, 0, time.UTC); !month.After(query.End); month = month.AddDate(0, 1, 0) {
		months = append(months, month.Format("2006-01-02"))
	}
	appointments := make([][]clients.AMDAppointmentResponse, len(months))
	holds := make([][]domain.BlockHold, len(months))
	failures := make([]error, 2*len(months))
	var reads sync.WaitGroup
	for i, month := range months {
		reads.Add(2)
		go func() {
			defer reads.Done()
			appointments[i], failures[2*i] = a.restClient.GetAppointmentsByMonth(ctx, token, columnIDs, month)
		}()
		go func() {
			defer reads.Done()
			holds[i], failures[2*i+1] = a.restClient.GetBlockHoldsByMonth(ctx, token, columnIDs, month)
		}()
	}
	reads.Wait()
	for _, err := range failures {
		if err != nil {
			failure := a.classify(token, err)
			log.Printf("schedule read incomplete category=%s", CategoryOf(failure))
			return nil, failure
		}
	}
	var scheduled []domain.Appointment
	for i, month := range months {
		rows, err := clients.ScheduledAppointments(appointments[i])
		if err != nil {
			failure := a.classify(token, err)
			log.Printf("schedule read incomplete category=%s", CategoryOf(failure))
			return nil, failure
		}
		for _, row := range rows {
			if row.StartDateTime.Format("2006-01") == month[:len("2006-01")] {
				scheduled = append(scheduled, row)
			}
		}
	}
	return scheduleByDate(query, scheduled, slices.Concat(holds...)), nil
}

func scheduleByDate(query domain.ScheduleRangeQuery, appointments []domain.Appointment, holds []domain.BlockHold) map[string]domain.ScheduleReadResult {
	queried := make(map[int]string, len(query.ColumnIDs))
	for _, columnID := range query.ColumnIDs {
		if id, err := strconv.Atoi(columnID); err == nil {
			queried[id] = columnID
		}
	}
	result := make(map[string]domain.ScheduleReadResult)
	unattributed := make(map[string]bool)
	for day := query.Start; !day.After(query.End); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		columns := make(map[string]domain.ColumnSchedule, len(query.ColumnIDs))
		for _, columnID := range query.ColumnIDs {
			columns[columnID] = domain.ColumnSchedule{AppointmentsComplete: true, BlockHoldsComplete: true}
		}
		result[date] = domain.ScheduleReadResult{Columns: columns}
	}
	for _, appointment := range distinctRows(appointments, appointmentKey) {
		date := appointment.StartDateTime.Format("2006-01-02")
		read, ok := result[date]
		if !ok {
			continue
		}
		columnID, ok := queried[appointment.ColumnID]
		if !ok {
			unattributed[date] = true
			continue
		}
		column := read.Columns[columnID]
		column.Appointments = append(column.Appointments, appointment)
		read.Columns[columnID] = column
	}
	for _, hold := range distinctRows(holds, holdKey) {
		span := hold.Interval()
		if !span.End.After(span.Start) {
			span.End = span.Start.Add(time.Nanosecond)
		}
		for date, read := range result {
			dayStart, _ := time.Parse("2006-01-02", date)
			if !span.Overlaps(domain.Interval{Start: dayStart, End: dayStart.AddDate(0, 0, 1)}) {
				continue
			}
			columnID, ok := queried[hold.ColumnID]
			if !ok {
				unattributed[date] = true
				continue
			}
			column := read.Columns[columnID]
			column.BlockHolds = append(column.BlockHolds, hold)
			read.Columns[columnID] = column
		}
	}
	for date := range unattributed {
		for columnID, column := range result[date].Columns {
			column.AppointmentsComplete = false
			column.BlockHoldsComplete = false
			result[date].Columns[columnID] = column
		}
	}
	return result
}

type rowKey struct {
	id     int
	column int
	start  time.Time
}

func appointmentKey(appointment domain.Appointment) rowKey {
	return rowKey{appointment.ID, appointment.ColumnID, appointment.StartDateTime}
}

func holdKey(hold domain.BlockHold) rowKey {
	return rowKey{hold.ID, hold.ColumnID, hold.StartDateTime}
}

func distinctRows[T any](rows []T, key func(T) rowKey) []T {
	seen := make(map[rowKey]bool, len(rows))
	var distinct []T
	for _, row := range rows {
		key := key(row)
		if key.id <= 0 || !seen[key] {
			seen[key] = true
			distinct = append(distinct, row)
		}
	}
	return distinct
}

func (a *Adapter) BookAppointment(ctx context.Context, booking Booking) (int, error) {
	token, err := a.token(ctx)
	if err != nil {
		return 0, err
	}
	office, ok := domain.LookupOfficeByID(booking.OfficeID)
	if !ok {
		return 0, NewError(safeerrors.CategoryInternal)
	}
	facilityID, err := strconv.Atoi(office.FacilityID)
	if err != nil {
		return 0, NewError(safeerrors.CategoryInternal)
	}
	if booking.ProviderAppointmentTypeID <= 0 || booking.AppointmentColor == "" {
		return 0, NewError(safeerrors.CategoryInternal)
	}
	force := 0
	if booking.Force {
		force = 1
	}

	appointmentID, err := a.restClient.BookAppointment(ctx, token, clients.BookAppointmentParams{
		PatientID:     booking.PatientID,
		ColumnID:      booking.ColumnID,
		ProfileID:     booking.ProfileID,
		StartDatetime: domain.FormatSlotDateTime(booking.Start),
		Duration:      booking.Duration,
		AppointmentType: []struct {
			ID int `json:"id"`
		}{{ID: booking.ProviderAppointmentTypeID}},
		EpisodeID:  1,
		FacilityID: facilityID,
		Color:      booking.AppointmentColor,
		Force:      force,
		Comments:   booking.Comments,
	})
	if err != nil {
		return 0, a.classifyMutation(token, err)
	}
	return appointmentID, nil
}

func (a *Adapter) CancelAppointment(ctx context.Context, cancellation Cancellation) error {
	token, err := a.token(ctx)
	if err != nil {
		return err
	}
	if err := a.restClient.CancelAppointment(ctx, token, cancellation.AppointmentID); err != nil {
		return a.classifyMutation(token, err)
	}
	return nil
}

func (a *Adapter) token(ctx context.Context) (*session.TokenData, error) {
	if a.session == nil {
		return nil, NewError(safeerrors.CategoryUnavailable)
	}
	token, err := a.session.Get(ctx)
	if err != nil {
		return nil, NewError(safeerrors.CategoryUnavailable)
	}
	if token == nil {
		return nil, NewError(safeerrors.CategoryUnavailable)
	}
	return token, nil
}

func (a *Adapter) classify(token *session.TokenData, err error) error {
	return NewError(a.providerCategory(token, err))
}

func (a *Adapter) classifyMutation(token *session.TokenData, err error) error {
	category := a.providerCategory(token, err)
	if safeerrors.IsNotSent(err) || refusedWrite(category) {
		return NewError(category)
	}
	return NewAmbiguousWriteError(category)
}

func (a *Adapter) providerCategory(token *session.TokenData, err error) safeerrors.Category {
	category := safeerrors.Classify(err)
	if category == safeerrors.CategoryAuthentication {
		a.session.Reject(token)
	}
	return category
}

func visitProviderName(name string) string {
	if last, first, found := strings.Cut(name, ","); found {
		name = first + " " + last
	}
	return titleCase(strings.Join(strings.Fields(name), " "))
}

func titleCase(name string) string {
	if name == "" {
		return ""
	}
	return cases.Title(language.English).String(strings.ToLower(name))
}

var _ PatientRecords = (*Adapter)(nil)
var _ SchedulingRecords = (*Adapter)(nil)
