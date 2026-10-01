package advancedmd

import (
	"context"
	"errors"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

type Error struct {
	category       safeerrors.Category
	ambiguousWrite bool
}

type MutationFailure string

const (
	MutationRejected  MutationFailure = "rejected"
	MutationAmbiguous MutationFailure = "ambiguous_write"
)

func NewError(category safeerrors.Category) error {
	return &Error{category: category}
}

func NewAmbiguousWriteError(category safeerrors.Category) error {
	return &Error{category: category, ambiguousWrite: true}
}

func (e *Error) Error() string {
	return string(e.category)
}

func CategoryOf(err error) safeerrors.Category {
	var classified *Error
	if errors.As(err, &classified) {
		return classified.category
	}
	return safeerrors.CategoryInternal
}

func IsAmbiguousWrite(err error) bool {
	var classified *Error
	return errors.As(err, &classified) && classified.ambiguousWrite
}

func MutationFailureOf(err error) MutationFailure {
	if IsAmbiguousWrite(err) {
		return MutationAmbiguous
	}
	switch CategoryOf(err) {
	case safeerrors.CategoryAuthentication, safeerrors.CategoryConflict, safeerrors.CategoryRejected:
		return MutationRejected
	default:
		return ""
	}
}

type PatientRecords interface {
	ReadPatientCandidates(ctx context.Context, firstName, dob string) (domain.PatientCandidateRead, error)
	SearchPatients(ctx context.Context, search domain.PatientSearch) ([]domain.Patient, error)
	GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error)
	ReadPatientAppointments(ctx context.Context, query domain.PatientAppointmentsQuery) (AppointmentRead, error)
	CreatePatient(ctx context.Context, command domain.PatientCreate) (domain.CreatedPatient, error)
	AddPatientInsurance(ctx context.Context, command domain.PatientInsurance) error
	EndDatePatientInsurance(ctx context.Context, command domain.PatientInsuranceEnd) error
}

type SchedulingRecords interface {
	GetSchedulerSetup(ctx context.Context) (domain.SchedulerSetup, error)
	ReadSchedule(ctx context.Context, query domain.ScheduleReadQuery) (domain.ScheduleReadResult, error)
	ReadScheduleRange(ctx context.Context, query domain.ScheduleRangeQuery) (map[string]domain.ScheduleReadResult, error)
	GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error)
	ReadPatientAppointments(ctx context.Context, query domain.PatientAppointmentsQuery) (AppointmentRead, error)
	ReadPatientAppointmentsForMonth(ctx context.Context, query AppointmentMonthQuery) (AppointmentRead, error)
	ReadAppointmentState(ctx context.Context, query AppointmentStateQuery) (AppointmentState, error)
	BookAppointment(ctx context.Context, booking Booking) (int, error)
	CancelAppointment(ctx context.Context, cancellation Cancellation) error
}

type AppointmentRead struct {
	Appointments  []domain.PatientAppointment
	Complete      bool
	ProviderReads int
}

type AppointmentMonthQuery struct {
	PatientID string
	OfficeIDs []string
	Month     time.Time
}

type AppointmentStateQuery struct {
	AppointmentID int
	OfficeID      string
	Start         time.Time
}

type AppointmentState struct {
	Exists   bool
	Complete bool
}

type Booking struct {
	PatientID                 int
	OfficeID                  string
	ColumnID                  int
	ProfileID                 int
	Start                     time.Time
	Duration                  int
	AppointmentTypeID         int
	ProviderAppointmentTypeID int
	AppointmentColor          string
	Force                     bool
	Comments                  string
}

type Cancellation struct {
	PatientID     string
	AppointmentID int
	OfficeID      string
}
