package advancedmdtest

import (
	"context"
	"strconv"
	"sync"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

type AppointmentResult struct {
	Read advancedmd.AppointmentRead
	Err  error
}

type AppointmentStateResult struct {
	State advancedmd.AppointmentState
	Err   error
}

type PatientSearchStep struct {
	Patients []domain.Patient
	Err      error
}

type CandidateQuery struct {
	FirstName string
	DOB       string
}

type ColumnRows struct {
	Appointments []domain.Appointment
	BlockHolds   []domain.BlockHold
}

type ColumnDay struct {
	Date     string
	ColumnID string
}

type Adapter struct {
	scheduleMu               sync.Mutex
	CandidateReads           map[string]domain.PatientCandidateRead
	CandidateReadErrors      map[string]error
	CandidateQueries         []CandidateQuery
	PatientSearches          map[domain.PatientSearch][]domain.Patient
	PatientErrors            map[domain.PatientSearch]error
	PatientSearchSequence    map[domain.PatientSearch][]PatientSearchStep
	IncompletePatientSearch  map[domain.PatientSearch]bool
	Demographics             map[string]domain.PatientDemographics
	DemographicErrors        map[string]error
	DemographicErrorSequence map[string][]error
	DemographicsStarted      chan<- struct{}
	DemographicsRelease      <-chan struct{}
	AppointmentResults       map[string]AppointmentResult
	AppointmentsStarted      chan<- struct{}
	AppointmentsRelease      <-chan struct{}
	AppointmentDayQueries    []advancedmd.AppointmentDayQuery
	AppointmentStateResults  map[int]AppointmentStateResult
	AppointmentStateQueries  []advancedmd.AppointmentStateQuery
	CreatedPatient           domain.CreatedPatient
	CreatePatientError       error
	AddInsuranceError        error
	EndInsuranceError        error
	CreatePatientCalls       int
	SearchPatientCalls       int
	Insurances               []domain.PatientInsurance
	InsuranceEnds            []domain.PatientInsuranceEnd
	AddInsuranceCalls        int
	DemographicCalls         int
	AppointmentReadCalls     int
	EndInsuranceCalls        int
	SchedulerSetup           domain.SchedulerSetup
	SchedulerSetupError      error
	SchedulerSetupCalls      int
	Schedule                 map[string]map[string]ColumnRows
	ScheduleReadErrors       map[string]error
	ColumnReadErrors         map[ColumnDay]error
	IncompleteDays           map[string]bool
	ScheduleRangeQueries     []domain.ScheduleRangeQuery
	ColumnDayReads           []ColumnDay
	BookAppointmentID        int
	BookAppointmentErr       error
	Bookings                 []advancedmd.Booking
	CancelAppointmentErr     error
	Cancellations            []advancedmd.Cancellation
}

func NewAdapter() *Adapter {
	return &Adapter{
		CandidateReads:           make(map[string]domain.PatientCandidateRead),
		CandidateReadErrors:      make(map[string]error),
		PatientSearches:          make(map[domain.PatientSearch][]domain.Patient),
		PatientErrors:            make(map[domain.PatientSearch]error),
		PatientSearchSequence:    make(map[domain.PatientSearch][]PatientSearchStep),
		Demographics:             make(map[string]domain.PatientDemographics),
		DemographicErrors:        make(map[string]error),
		DemographicErrorSequence: make(map[string][]error),
		AppointmentResults:       make(map[string]AppointmentResult),
		AppointmentStateResults:  make(map[int]AppointmentStateResult),
		Schedule:                 make(map[string]map[string]ColumnRows),
		ScheduleReadErrors:       make(map[string]error),
		ColumnReadErrors:         make(map[ColumnDay]error),
		IncompleteDays:           make(map[string]bool),
	}
}

func internalError() error {
	return advancedmd.NewError(safeerrors.CategoryInternal)
}

func (a *Adapter) SearchPatients(_ context.Context, search domain.PatientSearch) (domain.PatientCandidateRead, error) {
	a.SearchPatientCalls++
	patients, err := a.PatientSearches[search], a.PatientErrors[search]
	if sequence := a.PatientSearchSequence[search]; len(sequence) > 0 {
		patients, err = sequence[0].Patients, sequence[0].Err
		a.PatientSearchSequence[search] = sequence[1:]
	}
	if err != nil {
		return domain.PatientCandidateRead{}, err
	}
	return domain.PatientCandidateRead{
		Patients: append([]domain.Patient{}, patients...),
		Complete: !a.IncompletePatientSearch[search],
	}, nil
}

func (a *Adapter) ReadPatientCandidates(_ context.Context, firstName, dob string) (domain.PatientCandidateRead, error) {
	a.CandidateQueries = append(a.CandidateQueries, CandidateQuery{FirstName: firstName, DOB: dob})
	if err := a.CandidateReadErrors[firstName]; err != nil {
		return domain.PatientCandidateRead{}, err
	}
	read, configured := a.CandidateReads[firstName]
	return domain.PatientCandidateRead{
		Patients: append([]domain.Patient{}, read.Patients...),
		Complete: read.Complete || !configured,
	}, nil
}

func (a *Adapter) GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error) {
	a.DemographicCalls++
	if err := waitBarrier(ctx, a.DemographicsStarted, a.DemographicsRelease); err != nil {
		return domain.PatientDemographics{}, err
	}
	if sequence := a.DemographicErrorSequence[patientID]; len(sequence) > 0 {
		err := sequence[0]
		a.DemographicErrorSequence[patientID] = sequence[1:]
		if err != nil {
			return domain.PatientDemographics{}, err
		}
	}
	if err := a.DemographicErrors[patientID]; err != nil {
		return domain.PatientDemographics{}, err
	}
	return a.Demographics[patientID], nil
}

func (a *Adapter) ReadPatientAppointments(ctx context.Context, query domain.PatientAppointmentsQuery) (advancedmd.AppointmentRead, error) {
	a.AppointmentReadCalls++
	if err := waitBarrier(ctx, a.AppointmentsStarted, a.AppointmentsRelease); err != nil {
		return advancedmd.AppointmentRead{}, err
	}
	if !numeric(query.PatientID) || len(query.OfficeIDs) == 0 || !knownOffices(query.OfficeIDs) {
		return advancedmd.AppointmentRead{}, internalError()
	}
	result, configured := a.AppointmentResults[query.PatientID]
	providerReads := max(result.Read.ProviderReads, 1)
	if result.Err != nil {
		return advancedmd.AppointmentRead{ProviderReads: providerReads}, result.Err
	}
	return advancedmd.AppointmentRead{
		Appointments:  append([]domain.PatientAppointment{}, result.Read.Appointments...),
		Complete:      result.Read.Complete || !configured,
		ProviderReads: providerReads,
	}, nil
}

func (a *Adapter) ReadPatientAppointmentsForDay(ctx context.Context, query advancedmd.AppointmentDayQuery) (advancedmd.AppointmentRead, error) {
	query.OfficeIDs = append([]string(nil), query.OfficeIDs...)
	a.AppointmentDayQueries = append(a.AppointmentDayQueries, query)
	if err := waitBarrier(ctx, a.AppointmentsStarted, a.AppointmentsRelease); err != nil {
		return advancedmd.AppointmentRead{}, err
	}
	if query.Day.IsZero() || !numeric(query.PatientID) {
		return advancedmd.AppointmentRead{}, internalError()
	}
	result, configured := a.AppointmentResults[query.PatientID]
	read := advancedmd.AppointmentRead{Appointments: []domain.PatientAppointment{}, Complete: true}
	for _, officeID := range query.OfficeIDs {
		if _, ok := domain.LookupOfficeByID(officeID); !ok {
			return read, internalError()
		}
		read.ProviderReads++
		if result.Err != nil {
			return read, result.Err
		}
	}
	read.Appointments = append(read.Appointments, result.Read.Appointments...)
	read.Complete = result.Read.Complete || !configured
	return read, nil
}

func numeric(id string) bool {
	_, err := strconv.Atoi(id)
	return err == nil
}

func knownOffices(officeIDs []string) bool {
	for _, officeID := range officeIDs {
		if _, ok := domain.LookupOfficeByID(officeID); !ok {
			return false
		}
	}
	return true
}

func waitBarrier(ctx context.Context, started chan<- struct{}, release <-chan struct{}) error {
	if started != nil {
		select {
		case started <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (a *Adapter) ReadAppointmentState(_ context.Context, query advancedmd.AppointmentStateQuery) (advancedmd.AppointmentState, error) {
	a.AppointmentStateQueries = append(a.AppointmentStateQueries, query)
	if _, ok := domain.LookupOfficeByID(query.OfficeID); !ok || query.AppointmentID <= 0 || query.Start.IsZero() {
		return advancedmd.AppointmentState{}, internalError()
	}
	result, configured := a.AppointmentStateResults[query.AppointmentID]
	if result.Err != nil {
		return advancedmd.AppointmentState{}, result.Err
	}
	if !configured {
		result.State.Complete = true
	}
	return result.State, nil
}

func (a *Adapter) GetSchedulerSetup(_ context.Context) (domain.SchedulerSetup, error) {
	a.SchedulerSetupCalls++
	if a.SchedulerSetupError != nil {
		return domain.SchedulerSetup{}, a.SchedulerSetupError
	}
	if len(a.SchedulerSetup.Columns) == 0 {
		return domain.SchedulerSetup{}, advancedmd.NewError(safeerrors.CategoryInvalidResponse)
	}
	return a.SchedulerSetup, nil
}

func (a *Adapter) columnFailure(date, columnID string) error {
	if err := a.ScheduleReadErrors[date]; err != nil {
		return err
	}
	return a.ColumnReadErrors[ColumnDay{Date: date, ColumnID: columnID}]
}

func (a *Adapter) columnSchedule(date, columnID string) domain.ColumnSchedule {
	rows := a.Schedule[date][columnID]
	return domain.ColumnSchedule{
		Appointments:         distinctRows(rows.Appointments, func(row domain.Appointment) rowKey { return rowKey{row.ID, row.StartDateTime} }),
		BlockHolds:           distinctRows(rows.BlockHolds, func(row domain.BlockHold) rowKey { return rowKey{row.ID, row.StartDateTime} }),
		AppointmentsComplete: true,
		BlockHoldsComplete:   true,
	}
}

func (a *Adapter) ReadColumnDay(_ context.Context, columnID, date string) (domain.ColumnSchedule, error) {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()
	a.ColumnDayReads = append(a.ColumnDayReads, ColumnDay{Date: date, ColumnID: columnID})
	if err := a.columnFailure(date, columnID); err != nil {
		return domain.ColumnSchedule{}, err
	}
	return a.columnSchedule(date, columnID), nil
}

func (a *Adapter) ReadScheduleRange(_ context.Context, query domain.ScheduleRangeQuery) (map[string]domain.ScheduleReadResult, error) {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()
	query.ColumnIDs = append([]string(nil), query.ColumnIDs...)
	a.ScheduleRangeQueries = append(a.ScheduleRangeQueries, query)
	results := make(map[string]domain.ScheduleReadResult)
	for day := query.Start; !day.After(query.End); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		result := domain.ScheduleReadResult{Columns: make(map[string]domain.ColumnSchedule, len(query.ColumnIDs))}
		for _, columnID := range query.ColumnIDs {
			if err := a.columnFailure(date, columnID); err != nil {
				return nil, err
			}
			column := a.columnSchedule(date, columnID)
			if a.IncompleteDays[date] {
				column.AppointmentsComplete = false
				column.BlockHoldsComplete = false
			}
			result.Columns[columnID] = column
		}
		results[date] = result
	}
	return results, nil
}

type rowKey struct {
	id    int
	start time.Time
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

func (a *Adapter) BookAppointment(_ context.Context, booking advancedmd.Booking) (int, error) {
	office, ok := domain.LookupOfficeByID(booking.OfficeID)
	if !ok || !numeric(office.FacilityID) || booking.ProviderAppointmentTypeID <= 0 || booking.AppointmentColor == "" {
		return 0, internalError()
	}
	a.Bookings = append(a.Bookings, booking)
	if a.BookAppointmentErr != nil {
		return 0, a.BookAppointmentErr
	}
	return a.BookAppointmentID, nil
}

func (a *Adapter) CancelAppointment(_ context.Context, cancellation advancedmd.Cancellation) error {
	a.Cancellations = append(a.Cancellations, cancellation)
	return a.CancelAppointmentErr
}

func (a *Adapter) CreatePatient(_ context.Context, command domain.PatientCreate) (domain.CreatedPatient, error) {
	if _, ok := domain.LookupOfficeByID(command.OfficeID); !ok {
		return domain.CreatedPatient{}, internalError()
	}
	a.CreatePatientCalls++
	if a.CreatePatientError != nil {
		return domain.CreatedPatient{}, a.CreatePatientError
	}
	return a.CreatedPatient, nil
}

func (a *Adapter) AddPatientInsurance(_ context.Context, command domain.PatientInsurance) error {
	a.AddInsuranceCalls++
	a.Insurances = append(a.Insurances, command)
	return a.AddInsuranceError
}

func (a *Adapter) EndDatePatientInsurance(_ context.Context, command domain.PatientInsuranceEnd) error {
	a.EndInsuranceCalls++
	a.InsuranceEnds = append(a.InsuranceEnds, command)
	return a.EndInsuranceError
}

var _ advancedmd.PatientRecords = (*Adapter)(nil)
var _ advancedmd.SchedulingRecords = (*Adapter)(nil)
