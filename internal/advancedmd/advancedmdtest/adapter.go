// Package advancedmdtest provides a deterministic adapter for the true
// external AdvancedMD seam.
package advancedmdtest

import (
	"context"
	"sync"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
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

// Adapter returns caller-controlled domain results without provider I/O.
type Adapter struct {
	scheduleMu               sync.Mutex
	PatientSearches          map[domain.PatientSearch][]domain.Patient
	PatientErrors            map[domain.PatientSearch]error
	PatientSearchSequence    map[domain.PatientSearch][]PatientSearchStep
	Demographics             map[string]domain.PatientDemographics
	DemographicErrors        map[string]error
	DemographicErrorSequence map[string][]error
	DemographicsStarted      chan<- struct{}
	DemographicsRelease      <-chan struct{}
	AppointmentResults       map[string]AppointmentResult
	AppointmentsStarted      chan<- struct{}
	AppointmentsRelease      <-chan struct{}
	AppointmentQueries       []domain.PatientAppointmentsQuery
	AppointmentMonthQueries  []advancedmd.AppointmentMonthQuery
	AppointmentStateResults  map[int]AppointmentStateResult
	AppointmentStateQueries  []advancedmd.AppointmentStateQuery
	CreatedPatient           domain.CreatedPatient
	CreatePatientError       error
	AddInsuranceError        error
	EndInsuranceError        error
	CreatePatientCalls       int
	SearchPatientCalls       int
	AddInsuranceCalls        int
	DemographicCalls         int
	AppointmentReadCalls     int
	EndInsuranceCalls        int
	SchedulerSetup           domain.SchedulerSetup
	SchedulerSetupError      error
	SchedulerSetupCalls      int
	ScheduleReads            map[string]domain.ScheduleReadResult
	ScheduleReadErrors       map[string]error
	ScheduleReadQueries      []domain.ScheduleReadQuery
	BookAppointmentID        int
	BookAppointmentErr       error
	Bookings                 []advancedmd.Booking
	CancelAppointmentErr     error
	Cancellations            []advancedmd.Cancellation
}

func NewAdapter() *Adapter {
	return &Adapter{
		PatientSearches:          make(map[domain.PatientSearch][]domain.Patient),
		PatientErrors:            make(map[domain.PatientSearch]error),
		PatientSearchSequence:    make(map[domain.PatientSearch][]PatientSearchStep),
		Demographics:             make(map[string]domain.PatientDemographics),
		DemographicErrors:        make(map[string]error),
		DemographicErrorSequence: make(map[string][]error),
		AppointmentResults:       make(map[string]AppointmentResult),
		AppointmentStateResults:  make(map[int]AppointmentStateResult),
		ScheduleReads:            make(map[string]domain.ScheduleReadResult),
		ScheduleReadErrors:       make(map[string]error),
	}
}

func (a *Adapter) SearchPatients(_ context.Context, search domain.PatientSearch) ([]domain.Patient, error) {
	a.SearchPatientCalls++
	if sequence := a.PatientSearchSequence[search]; len(sequence) > 0 {
		step := sequence[0]
		a.PatientSearchSequence[search] = sequence[1:]
		return append([]domain.Patient(nil), step.Patients...), step.Err
	}
	if err := a.PatientErrors[search]; err != nil {
		return nil, err
	}
	return append([]domain.Patient(nil), a.PatientSearches[search]...), nil
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
	if err := a.recordAppointmentRead(ctx, query); err != nil {
		return advancedmd.AppointmentRead{}, err
	}
	return a.nextAppointmentRead(query.PatientID)
}

func (a *Adapter) ReadPatientAppointmentsForMonth(_ context.Context, query advancedmd.AppointmentMonthQuery) (advancedmd.AppointmentRead, error) {
	query.OfficeIDs = append([]string(nil), query.OfficeIDs...)
	a.AppointmentMonthQueries = append(a.AppointmentMonthQueries, query)
	return a.nextAppointmentRead(query.PatientID)
}

func (a *Adapter) nextAppointmentRead(patientID string) (advancedmd.AppointmentRead, error) {
	result, configured := a.AppointmentResults[patientID]
	read := result.Read
	if read.ProviderReads == 0 {
		read.ProviderReads = 1
	}
	if result.Err != nil {
		return read, result.Err
	}
	read.Appointments = append([]domain.PatientAppointment(nil), read.Appointments...)
	if !configured {
		read.Complete = true
	}
	return read, nil
}

func (a *Adapter) recordAppointmentRead(ctx context.Context, query domain.PatientAppointmentsQuery) error {
	a.AppointmentReadCalls++
	query.OfficeIDs = append([]string(nil), query.OfficeIDs...)
	a.AppointmentQueries = append(a.AppointmentQueries, query)
	return waitBarrier(ctx, a.AppointmentsStarted, a.AppointmentsRelease)
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
	return a.SchedulerSetup, nil
}

func (a *Adapter) ReadSchedule(_ context.Context, query domain.ScheduleReadQuery) (domain.ScheduleReadResult, error) {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()
	query.ColumnIDs = append([]string(nil), query.ColumnIDs...)
	a.ScheduleReadQueries = append(a.ScheduleReadQueries, query)
	if err := a.ScheduleReadErrors[query.Date]; err != nil {
		return domain.ScheduleReadResult{}, err
	}
	read := a.ScheduleReads[query.Date]
	result := domain.ScheduleReadResult{
		Columns: make(map[string]domain.ColumnSchedule, len(query.ColumnIDs)),
	}
	for _, columnID := range query.ColumnIDs {
		if column, ok := read.Columns[columnID]; ok {
			result.Columns[columnID] = column
		}
	}
	return result, nil
}

func (a *Adapter) BookAppointment(_ context.Context, booking advancedmd.Booking) (int, error) {
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

func (a *Adapter) CreatePatient(_ context.Context, _ domain.PatientCreate) (domain.CreatedPatient, error) {
	a.CreatePatientCalls++
	return a.CreatedPatient, a.CreatePatientError
}

func (a *Adapter) AddPatientInsurance(_ context.Context, _ domain.PatientInsurance) error {
	a.AddInsuranceCalls++
	return a.AddInsuranceError
}

func (a *Adapter) EndDatePatientInsurance(_ context.Context, _ domain.PatientInsuranceEnd) error {
	a.EndInsuranceCalls++
	return a.EndInsuranceError
}

var _ advancedmd.PatientRecords = (*Adapter)(nil)
var _ advancedmd.SchedulingRecords = (*Adapter)(nil)
