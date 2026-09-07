// Package advancedmd defines the domain-oriented seam around the external
// AdvancedMD dependency.
package advancedmd

import (
	"context"
	"errors"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

// Error preserves a stable category while keeping provider details behind the
// AdvancedMD seam.
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

// NewAmbiguousWriteError reports that the provider may have applied a mutation
// even though the adapter could not observe a definitive response.
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

// IsAmbiguousWrite reports whether reconciliation is required before the
// caller can declare a provider mutation successful or failed.
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

// PatientRecords is the AdvancedMD surface required by the Patient module.
// Implementations own authentication, provider endpoints, request formats, and
// response parsing.
type PatientRecords interface {
	SearchPatients(ctx context.Context, search domain.PatientSearch) ([]domain.Patient, error)
	GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error)
	ReadPatientAppointments(ctx context.Context, query domain.PatientAppointmentsQuery) (AppointmentRead, error)
	CreatePatient(ctx context.Context, command domain.PatientCreate) (domain.CreatedPatient, error)
	AddPatientInsurance(ctx context.Context, command domain.PatientInsurance) error
	EndDatePatientInsurance(ctx context.Context, command domain.PatientInsuranceEnd) error
}

// SchedulingRecords is the AdvancedMD surface required by the Scheduling
// module. Implementations keep authentication, endpoints, and provider payloads
// behind domain scheduler setup and schedule-read results.
type SchedulingRecords interface {
	GetSchedulerSetup(ctx context.Context) (domain.SchedulerSetup, error)
	ReadSchedule(ctx context.Context, query domain.ScheduleReadQuery) (domain.ScheduleReadResult, error)
	GetPatientDemographics(ctx context.Context, patientID string) (domain.PatientDemographics, error)
	ReadPatientAppointments(ctx context.Context, query domain.PatientAppointmentsQuery) (AppointmentRead, error)
	ReadPatientAppointmentsForMonth(ctx context.Context, query AppointmentMonthQuery) (AppointmentRead, error)
	ReadAppointmentState(ctx context.Context, query AppointmentStateQuery) (AppointmentState, error)
	BookAppointment(ctx context.Context, booking Booking) (int, error)
	CancelAppointment(ctx context.Context, cancellation Cancellation) error
}

// AppointmentRead distinguishes a complete provider snapshot from a partial
// parse that cannot safely prove the absence of an appointment.
type AppointmentRead struct {
	Appointments []domain.PatientAppointment
	Complete     bool
	// ProviderReads is the number of provider schedule requests attempted.
	ProviderReads int
}

// AppointmentMonthQuery selects the provider month containing an intended
// booking so reconciliation does not depend on a rolling upcoming window.
type AppointmentMonthQuery struct {
	PatientID string
	OfficeIDs []string
	Month     time.Time
}

// AppointmentStateQuery identifies the provider month that owns an appointment.
type AppointmentStateQuery struct {
	AppointmentID int
	OfficeID      string
	Start         time.Time
}

// AppointmentState proves whether a known appointment remains in the
// provider's current schedule.
type AppointmentState struct {
	Exists   bool
	Complete bool
}

// Booking is the policy-resolved scheduling decision sent through the
// AdvancedMD seam. Scheduling policy owns provider appointment type IDs and
// colors; the production adapter owns payload shape and transport.
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

// Cancellation is the verified Acuity context for one provider cancellation.
// The production adapter owns which fields the provider transport requires.
type Cancellation struct {
	PatientID     string
	AppointmentID int
	OfficeID      string
}
