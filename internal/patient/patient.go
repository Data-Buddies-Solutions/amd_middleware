// Package patient owns patient resolution and construction of complete Acuity
// patient results.
package patient

import (
	"context"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

type Status string

const (
	StatusVerified        Status = "verified"
	StatusCandidate       Status = "candidate"
	StatusMultipleMatches Status = "multiple_matches"
	StatusNotFound        Status = "not_found"
	StatusUnresolved      Status = "unresolved"
)

type AppointmentsStatus string

const (
	AppointmentsFound AppointmentsStatus = "found"
	AppointmentsNone  AppointmentsStatus = "none"
	AppointmentsError AppointmentsStatus = "error"
)

type CreateStatus string

const (
	CreateStatusCreated CreateStatus = "created"
	CreateStatusPartial CreateStatus = "partial"
	CreateStatusError   CreateStatus = "error"
)

type MutationOutcome string

const (
	MutationRejected           MutationOutcome = "rejected"
	MutationReconciledFailure  MutationOutcome = "reconciled_failure"
	MutationIndeterminateWrite MutationOutcome = "indeterminate_write"
	MutationValidationFailed   MutationOutcome = "validation_failed"
	MutationUnavailable        MutationOutcome = "unavailable"
	MutationFailed             MutationOutcome = "failed"
	MutationReconciledSuccess  MutationOutcome = "reconciled_success"
)

type UpdateInsuranceStatus string

const (
	UpdateInsuranceStatusUpdated UpdateInsuranceStatus = "updated"
	UpdateInsuranceStatusError   UpdateInsuranceStatus = "error"
)

// ResolveCommand is the complete patient-resolution intent accepted from the
// HTTP adapter after transport validation.
type ResolveCommand struct {
	PatientID string
	LastName  string
	DOB       string
	FirstName string
	Phone     string
	OfficeID  string
}

type Appointment struct {
	ID                int
	Date              string
	Time              string
	Provider          string
	Type              string
	VisitType         string `json:"visitType,omitempty"`
	AppointmentTypeID int
	Facility          string
	OfficeID          string
	Office            string
	CancellationToken string
	RescheduleToken   string
}

// Candidate contains only the private identity-selection facts available
// before one patient is fully hydrated.
type Candidate struct {
	Status    Status
	PatientID string
	FirstName string
	LastName  string
	DOB       string
}

// ResolveResult is one complete Acuity patient resolution outcome.
type ResolveResult struct {
	InsuranceDecision   *domain.InsuranceDecision
	Reason              string
	Status              Status
	ProviderFailure     safeerrors.Category
	PatientID           string
	FirstName           string
	LastName            string
	Name                string
	DOB                 string
	Phone               string
	InsuranceCarrier    string
	InsuranceCarrierID  string
	InsPlanID           string
	RespPartyID         string
	Routing             domain.RoutingRule
	AllowedProviders    []string
	RoutingAmbiguous    bool
	PreauthRequired     bool
	AppointmentsStatus  AppointmentsStatus
	Appointments        []Appointment
	AppointmentsMessage string
	Message             string
	Matches             []Candidate
	Observation         ResolutionObservation
}

// ResolutionObservation contains only PHI-free operational facts for one
// patient-resolution request.
type ResolutionObservation struct {
	Recorded                bool
	PatientSearchDurationMS int64
	DemographicDurationMS   int64
	AppointmentDurationMS   int64
	PatientSearchReads      int
	DemographicReads        int
	AppointmentReads        int
	OfficeGroupSize         int
	CandidateCountBucket    string
	AppointmentOutcome      string
}

// CreateCommand is the complete caller intent for creating a patient and
// attaching primary insurance.
type CreateCommand struct {
	FirstName      string
	LastName       string
	DOB            string
	Phone          string
	Email          string
	Street         string
	AptSuite       string
	City           string
	State          string
	Zip            string
	Sex            string
	SSN            string
	Insurance      string
	CoverageType   string
	SubscriberName string
	SubscriberNum  string
	Office         string
}

// CreateResult preserves the public patient-creation response contract.
type CreateResult struct {
	InsuranceDecision *domain.InsuranceDecision
	Status            CreateStatus
	Outcome           MutationOutcome
	PatientID         string
	Name              string
	DOB               string
	Routing           domain.RoutingRule
	AllowedProviders  []string
	PreauthRequired   bool
	Message           string
}

// UpdateInsuranceCommand is the complete caller intent for replacing primary
// insurance.
type UpdateInsuranceCommand struct {
	PatientID      string
	DOB            string
	InsPlanID      string
	RespPartyID    string
	OldInsurance   string
	Insurance      string
	CoverageType   string
	SubscriberName string
	SubscriberNum  string
	Office         string
}

// UpdateInsuranceResult preserves the public insurance-update response
// contract.
type UpdateInsuranceResult struct {
	InsuranceDecision *domain.InsuranceDecision
	Status            UpdateInsuranceStatus
	Outcome           MutationOutcome
	PatientID         string
	OldInsurance      string
	NewInsurance      string
	Routing           domain.RoutingRule
	AllowedProviders  []string
	RoutingAmbiguous  bool
	PreauthRequired   bool
	Message           string
}

// Patient is the single interface used by patient-facing HTTP routes.
type Patient interface {
	Resolve(context.Context, ResolveCommand) (ResolveResult, error)
	Create(context.Context, CreateCommand) CreateResult
	UpdateInsurance(context.Context, UpdateInsuranceCommand) UpdateInsuranceResult
}

type patient struct {
	advancedMD        advancedmd.PatientRecords
	appointmentTokens AppointmentTokenIssuer
}

type AppointmentTokenIssuer interface {
	IssueCancellationToken(string, domain.PatientAppointment) (string, error)
	IssueRescheduleToken(string, domain.PatientAppointment) (string, error)
}

func New(advancedMD advancedmd.PatientRecords) Patient {
	return &patient{advancedMD: advancedMD}
}

func NewWithAppointmentTokens(
	advancedMD advancedmd.PatientRecords,
	appointmentTokens AppointmentTokenIssuer,
) Patient {
	return &patient{
		advancedMD:        advancedMD,
		appointmentTokens: appointmentTokens,
	}
}
