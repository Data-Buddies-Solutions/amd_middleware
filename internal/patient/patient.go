package patient

import (
	"context"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
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

type ResolveCommand struct {
	PatientID string `json:"patientId"`
	LastName  string `json:"lastName"`
	DOB       string `json:"dob"`
	FirstName string `json:"firstName"`
	Phone     string `json:"phone"`
	Office    string `json:"office"`
	OfficeID  string `json:"-"`
}

type Appointment struct {
	ID                int    `json:"id"`
	Date              string `json:"date"`
	Time              string `json:"time"`
	Provider          string `json:"provider,omitempty"`
	Type              string `json:"type,omitempty"`
	VisitType         string `json:"visitType,omitempty"`
	AppointmentTypeID int    `json:"appointmentTypeId,omitempty"`
	Facility          string `json:"facility,omitempty"`
	OfficeID          string `json:"officeId,omitempty"`
	Office            string `json:"office,omitempty"`
	CancellationToken string `json:"cancellationToken,omitempty"`
	RescheduleToken   string `json:"rescheduleToken,omitempty"`
}

type Candidate struct {
	Status    Status `json:"status"`
	PatientID string `json:"patientId"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	DOB       string `json:"dob"`
}

type ResolveResult struct {
	InsuranceDecision   *insurance.InsuranceDecision `json:"insuranceDecision,omitempty"`
	Reason              string                       `json:"reason,omitempty"`
	Status              Status                       `json:"status"`
	ProviderFailure     safeerrors.Category          `json:"-"`
	PatientID           string                       `json:"patientId,omitempty"`
	FirstName           string                       `json:"-"`
	LastName            string                       `json:"-"`
	Name                string                       `json:"name,omitempty"`
	DOB                 string                       `json:"dob,omitempty"`
	Phone               string                       `json:"phone,omitempty"`
	InsuranceCarrier    string                       `json:"insuranceCarrier,omitempty"`
	InsuranceCarrierID  string                       `json:"insuranceCarrierId,omitempty"`
	InsPlanID           string                       `json:"insPlanId,omitempty"`
	RespPartyID         string                       `json:"respPartyId,omitempty"`
	Routing             domain.RoutingRule           `json:"routing,omitempty"`
	AllowedProviders    []string                     `json:"allowedProviders,omitempty"`
	RoutingAmbiguous    bool                         `json:"routingAmbiguous,omitempty"`
	PreauthRequired     bool                         `json:"preauthRequired,omitempty"`
	AppointmentsStatus  AppointmentsStatus           `json:"appointmentsStatus,omitempty"`
	Appointments        []Appointment                `json:"appointments"`
	AppointmentsMessage string                       `json:"appointmentsMessage,omitempty"`
	Message             string                       `json:"message,omitempty"`
	Matches             []Candidate                  `json:"matches"`
	Observation         ResolutionObservation        `json:"-"`
}

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

type CreateCommand struct {
	FirstName      string `json:"firstName"`
	LastName       string `json:"lastName"`
	DOB            string `json:"dob"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Street         string `json:"street"`
	AptSuite       string `json:"aptSuite"`
	City           string `json:"city"`
	State          string `json:"state"`
	Zip            string `json:"zip"`
	Sex            string `json:"sex"`
	SSN            string `json:"ssn"`
	Insurance      string `json:"insurance"`
	CoverageType   string `json:"coverageType"`
	SubscriberName string `json:"subscriberName"`
	SubscriberNum  string `json:"subscriberNum"`
	Office         string `json:"office"`
}

type CreateResult struct {
	InsuranceDecision *insurance.InsuranceDecision `json:"insuranceDecision,omitempty"`
	Status            CreateStatus                 `json:"status"`
	Outcome           MutationOutcome              `json:"outcome,omitempty"`
	PatientID         string                       `json:"patientId,omitempty"`
	Name              string                       `json:"name,omitempty"`
	DOB               string                       `json:"dob,omitempty"`
	Routing           domain.RoutingRule           `json:"routing,omitempty"`
	AllowedProviders  []string                     `json:"allowedProviders,omitempty"`
	PreauthRequired   bool                         `json:"preauthRequired,omitempty"`
	Message           string                       `json:"message,omitempty"`
}

type UpdateInsuranceCommand struct {
	PatientID      string `json:"patientId"`
	DOB            string `json:"dob"`
	InsPlanID      string `json:"insPlanId"`
	RespPartyID    string `json:"respPartyId"`
	OldInsurance   string `json:"oldInsurance"`
	Insurance      string `json:"insurance"`
	CoverageType   string `json:"coverageType"`
	SubscriberName string `json:"subscriberName"`
	SubscriberNum  string `json:"subscriberNum"`
	Office         string `json:"office"`
}

type UpdateInsuranceResult struct {
	Effect            string                       `json:"effect"`
	InsuranceDecision *insurance.InsuranceDecision `json:"insuranceDecision,omitempty"`
	Status            UpdateInsuranceStatus        `json:"status"`
	Outcome           MutationOutcome              `json:"outcome,omitempty"`
	PatientID         string                       `json:"patientId,omitempty"`
	OldInsurance      string                       `json:"oldInsurance,omitempty"`
	NewInsurance      string                       `json:"newInsurance,omitempty"`
	Routing           domain.RoutingRule           `json:"routing,omitempty"`
	AllowedProviders  []string                     `json:"allowedProviders,omitempty"`
	RoutingAmbiguous  bool                         `json:"routingAmbiguous,omitempty"`
	PreauthRequired   bool                         `json:"preauthRequired,omitempty"`
	Message           string                       `json:"message,omitempty"`
}

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

func New(advancedMD advancedmd.PatientRecords, appointmentTokens AppointmentTokenIssuer) Patient {
	return &patient{
		advancedMD:        advancedMD,
		appointmentTokens: appointmentTokens,
	}
}
