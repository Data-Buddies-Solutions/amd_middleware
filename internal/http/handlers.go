package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	patientmodule "advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

// ErrorResponse is the JSON response structure for error conditions.
// Returns 200 OK with status:"error" so ElevenLabs passes the message to the LLM.
type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// PatientResolveRequest is the single patient-read request shape. It supports
// pre-call phone lookup, verification by phone/name/DOB, and direct loading for
// an already verified patient ID.
type PatientResolveRequest struct {
	PatientID string `json:"patientId,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	DOB       string `json:"dob,omitempty"`
	FirstName string `json:"firstName,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Office    string `json:"office,omitempty"`
}

// PatientResolveResponse is returned by /api/patient/resolve.
type PatientResolveResponse struct {
	Status              string                     `json:"status"`
	PatientID           string                     `json:"patientId,omitempty"`
	Name                string                     `json:"name,omitempty"`
	DOB                 string                     `json:"dob,omitempty"`
	Phone               string                     `json:"phone,omitempty"`
	InsuranceCarrier    string                     `json:"insuranceCarrier,omitempty"`
	InsuranceCarrierID  string                     `json:"insuranceCarrierId,omitempty"`
	InsPlanID           string                     `json:"insPlanId,omitempty"`
	RespPartyID         string                     `json:"respPartyId,omitempty"`
	Routing             string                     `json:"routing,omitempty"`
	AllowedProviders    []string                   `json:"allowedProviders,omitempty"`
	RoutingAmbiguous    bool                       `json:"routingAmbiguous,omitempty"`
	PreauthRequired     bool                       `json:"preauthRequired,omitempty"`
	AppointmentsStatus  string                     `json:"appointmentsStatus,omitempty"`
	Appointments        []PatientApptDetail        `json:"appointments"`
	AppointmentsMessage string                     `json:"appointmentsMessage,omitempty"`
	Message             string                     `json:"message,omitempty"`
	Matches             []PatientCandidateResponse `json:"matches,omitempty"`
}

// PatientCandidateResponse is the private, lightweight identity selection
// shape returned before one patient is fully hydrated.
type PatientCandidateResponse struct {
	Status    string `json:"status"`
	PatientID string `json:"patientId"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	DOB       string `json:"dob"`
}

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	session    session.Session
	patient    patientmodule.Patient
	scheduling schedulingmodule.Scheduling
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(
	amdSession session.Session,
	patient patientmodule.Patient,
	scheduling schedulingmodule.Scheduling,
) *Handlers {
	return &Handlers{
		session:    amdSession,
		patient:    patient,
		scheduling: scheduling,
	}
}

// HandleLive reports process liveness without calling AdvancedMD.
func (h *Handlers) HandleLive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// HandleReady reports that local initialization completed and HTTP traffic can
// be accepted. AdvancedMD session health remains a separate signal.
func (h *Handlers) HandleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ready"}`))
}

// HandleMetrics exposes PHI-free patient mutation outcome counters.
func (h *Handlers) HandleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintln(w, "# HELP patient_mutation_outcomes_total Patient mutation outcomes by operation and category.")
	fmt.Fprintln(w, "# TYPE patient_mutation_outcomes_total counter")
	for _, metric := range patientmodule.MutationMetricSnapshot() {
		fmt.Fprintf(
			w,
			"patient_mutation_outcomes_total{operation=%q,outcome=%q} %d\n",
			metric.Operation,
			metric.Outcome,
			metric.Count,
		)
	}
}

// HandleSessionMaintenance refreshes the process-local AdvancedMD session
// without returning credentials, tokens, or provider endpoints.
func (h *Handlers) HandleSessionMaintenance(w http.ResponseWriter, r *http.Request) {
	if err := h.session.Maintain(r.Context()); err != nil {
		category := safeerrors.Classify(err)
		recordRequestOutcome(r.Context(), outcomeProviderFailure, category)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable"}`))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddPatientRequest is the expected JSON body for patient creation.
type AddPatientRequest struct {
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
	SSN            string `json:"ssn,omitempty"`
	Insurance      string `json:"insurance"`
	CoverageType   string `json:"coverageType,omitempty"`
	SubscriberName string `json:"subscriberName"`
	SubscriberNum  string `json:"subscriberNum"`
	Office         string `json:"office,omitempty"`
}

// AddPatientResponse is returned after creating a patient.
type AddPatientResponse struct {
	Status           string   `json:"status"`
	Outcome          string   `json:"outcome,omitempty"`
	PatientID        string   `json:"patientId,omitempty"`
	Name             string   `json:"name,omitempty"`
	DOB              string   `json:"dob,omitempty"`
	Routing          string   `json:"routing,omitempty"`
	AllowedProviders []string `json:"allowedProviders,omitempty"`
	PreauthRequired  bool     `json:"preauthRequired,omitempty"`
	Message          string   `json:"message,omitempty"`
}

// HandleAddPatient creates a new patient in AdvancedMD and attaches insurance.
func (h *Handlers) HandleAddPatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req AddPatientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	result := h.patient.Create(r.Context(), patientmodule.CreateCommand{
		FirstName:      req.FirstName,
		LastName:       req.LastName,
		DOB:            req.DOB,
		Phone:          req.Phone,
		Email:          req.Email,
		Street:         req.Street,
		AptSuite:       req.AptSuite,
		City:           req.City,
		State:          req.State,
		Zip:            req.Zip,
		Sex:            req.Sex,
		SSN:            req.SSN,
		Insurance:      req.Insurance,
		CoverageType:   req.CoverageType,
		SubscriberName: req.SubscriberName,
		SubscriberNum:  req.SubscriberNum,
		Office:         req.Office,
	})
	recordPatientMutationOutcome(r.Context(), result.Outcome)
	outcome := ""
	if result.Status != patientmodule.CreateStatusCreated {
		outcome = string(result.Outcome)
	}
	json.NewEncoder(w).Encode(AddPatientResponse{
		Status:           string(result.Status),
		Outcome:          outcome,
		PatientID:        result.PatientID,
		Name:             result.Name,
		DOB:              result.DOB,
		Routing:          string(result.Routing),
		AllowedProviders: result.AllowedProviders,
		PreauthRequired:  result.PreauthRequired,
		Message:          result.Message,
	})
}

// PatientApptDetail is one appointment returned to the Voice Agent.
type PatientApptDetail struct {
	ID                int    `json:"id"`                          // AMD appointment ID — for cancel_appt
	Date              string `json:"date"`                        // Human-readable (e.g., "Wednesday, March 18, 2026")
	Time              string `json:"time"`                        // e.g., "12:00 PM"
	Provider          string `json:"provider,omitempty"`          // e.g., "Dr. Austin Bach"
	Type              string `json:"type,omitempty"`              // e.g., "New Adult Medical"
	AppointmentTypeID int    `json:"appointmentTypeId,omitempty"` // AMD appointment type ID
	Facility          string `json:"facility,omitempty"`          // e.g., "Abita Eye Group Spring Hill"
	OfficeID          string `json:"officeId,omitempty"`          // Stable office ID that owns the appointment column
	Office            string `json:"office,omitempty"`            // Display name for the owning office
	CancellationToken string `json:"cancellationToken,omitempty"` // Private agent-owned cancellation authorization
	RescheduleToken   string `json:"rescheduleToken,omitempty"`   // Private agent-owned reschedule authorization
}

// HandlePatientResolve resolves a patient and, by default, loads appointments.
func (h *Handlers) HandlePatientResolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req PatientResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	office, err := domain.ResolveOffice(req.Office)
	if err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	if msg := validatePatientResolveRequest(req); msg != "" {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: msg,
		})
		return
	}

	if h.patient == nil {
		recordRequestOutcome(r.Context(), outcomeInternalFailure, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: "Failed to look up patient in AdvancedMD. Please try again.",
		})
		return
	}

	result, err := h.patient.Resolve(r.Context(), patientmodule.ResolveCommand{
		PatientID: req.PatientID,
		LastName:  req.LastName,
		DOB:       req.DOB,
		FirstName: req.FirstName,
		Phone:     req.Phone,
		OfficeID:  office.ID,
	})
	recordPatientResolutionObservation(r.Context(), result.Observation)
	if err != nil {
		category := advancedmd.CategoryOf(err)
		recordRequestOutcome(r.Context(), outcomeProviderFailure, category)
		message := "Failed to look up patient in AdvancedMD. Please try again."
		if category == safeerrors.CategoryAuthentication || category == safeerrors.CategoryUnavailable {
			message = "Service authentication is temporarily unavailable. Please try again."
		}
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: message,
		})
		return
	}
	if result.ProviderFailure != "" && result.ProviderFailure != safeerrors.CategoryNone {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, result.ProviderFailure)
	}

	json.NewEncoder(w).Encode(patientResolveResponse(result))
}

func patientResolveResponse(result patientmodule.ResolveResult) PatientResolveResponse {
	appointments := make([]PatientApptDetail, len(result.Appointments))
	for i, appointment := range result.Appointments {
		appointments[i] = PatientApptDetail{
			ID:                appointment.ID,
			Date:              appointment.Date,
			Time:              appointment.Time,
			Provider:          appointment.Provider,
			Type:              appointment.Type,
			AppointmentTypeID: appointment.AppointmentTypeID,
			Facility:          appointment.Facility,
			OfficeID:          appointment.OfficeID,
			Office:            appointment.Office,
			CancellationToken: appointment.CancellationToken,
			RescheduleToken:   appointment.RescheduleToken,
		}
	}
	matches := make([]PatientCandidateResponse, len(result.Matches))
	for i, match := range result.Matches {
		matches[i] = PatientCandidateResponse{
			Status:    string(match.Status),
			PatientID: match.PatientID,
			FirstName: match.FirstName,
			LastName:  match.LastName,
			DOB:       match.DOB,
		}
	}
	return PatientResolveResponse{
		Status:              string(result.Status),
		PatientID:           result.PatientID,
		Name:                result.Name,
		DOB:                 result.DOB,
		Phone:               result.Phone,
		InsuranceCarrier:    result.InsuranceCarrier,
		InsuranceCarrierID:  result.InsuranceCarrierID,
		InsPlanID:           result.InsPlanID,
		RespPartyID:         result.RespPartyID,
		Routing:             string(result.Routing),
		AllowedProviders:    result.AllowedProviders,
		RoutingAmbiguous:    result.RoutingAmbiguous,
		PreauthRequired:     result.PreauthRequired,
		AppointmentsStatus:  string(result.AppointmentsStatus),
		Appointments:        appointments,
		AppointmentsMessage: result.AppointmentsMessage,
		Message:             result.Message,
		Matches:             matches,
	}
}

func validatePatientResolveRequest(req PatientResolveRequest) string {
	hasPatientID := req.PatientID != ""
	hasLookupFields := req.Phone != "" || req.FirstName != "" || req.LastName != "" || req.DOB != ""
	if hasPatientID {
		if _, err := strconv.Atoi(req.PatientID); err != nil {
			return "patientId must be numeric"
		}
		if hasLookupFields {
			return "Provide either patientId or lookup fields, not both"
		}
		return ""
	}
	if req.Phone != "" {
		if domain.NormalizePhoneDigits(req.Phone) == "" {
			return "phone must contain at least one digit"
		}
		return ""
	}
	if req.LastName != "" && req.DOB != "" {
		return ""
	}
	return "Provide patientId, phone, phone + firstName, phone + dob, or lastName + dob"
}

type CancelAppointmentRequest struct {
	AppointmentID     int    `json:"appointmentId,omitempty"`
	PatientID         string `json:"patientId,omitempty"`
	Office            string `json:"office,omitempty"`
	CancellationToken string `json:"cancellationToken,omitempty"`

	cancellationTokenProvided bool
}

func (r *CancelAppointmentRequest) UnmarshalJSON(data []byte) error {
	type request CancelAppointmentRequest
	var decoded request
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = CancelAppointmentRequest(decoded)
	_, r.cancellationTokenProvided = fields["cancellationToken"]
	return nil
}

func (r CancelAppointmentRequest) command() schedulingmodule.CancelCommand {
	var cancellationToken *string
	if r.cancellationTokenProvided {
		cancellationToken = &r.CancellationToken
	}
	return schedulingmodule.CancelCommand{
		AppointmentID:     r.AppointmentID,
		PatientID:         r.PatientID,
		Office:            r.Office,
		CancellationToken: cancellationToken,
	}
}

type CancelAppointmentResponse = schedulingmodule.CancelReceipt

// HandleCancelAppointment delegates cancellation behavior to Scheduling.
func (h *Handlers) HandleCancelAppointment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req CancelAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}
	if h.scheduling == nil {
		recordRequestOutcome(r.Context(), outcomeInternalFailure, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Outcome: string(schedulingmodule.CategoryWriteFailed),
			Message: "Appointment scheduling is temporarily unavailable. Please try again.",
		})
		return
	}
	ctx := schedulingmodule.WithCancellationObserver(
		r.Context(),
		func(observation schedulingmodule.CancellationObservation) {
			recordCancellationObservation(r.Context(), observation)
		},
	)
	response, err := h.scheduling.Cancel(ctx, req.command())
	if err != nil {
		recordSchedulingError(r.Context(), err)
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Outcome: schedulingOutcome(err),
			Message: err.Error(),
		})
		return
	}
	json.NewEncoder(w).Encode(response)
}

type BookAppointmentRequest = schedulingmodule.BookCommand
type BookAppointmentResponse = schedulingmodule.BookReceipt

// HandleBookAppointment delegates booking behavior to Scheduling.
func (h *Handlers) HandleBookAppointment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req BookAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}
	if h.scheduling == nil {
		recordRequestOutcome(r.Context(), outcomeInternalFailure, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Outcome: string(schedulingmodule.CategoryWriteFailed),
			Message: "Appointment scheduling is temporarily unavailable. Please try again.",
		})
		return
	}
	response, err := h.scheduling.Book(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Outcome: schedulingOutcome(err),
			Message: err.Error(),
			Missing: schedulingmodule.MissingOf(err),
		})
		return
	}
	json.NewEncoder(w).Encode(response)
}

func schedulingOutcome(err error) string {
	category := schedulingmodule.CategoryOf(err)
	if category == schedulingmodule.CategoryValidation {
		return ""
	}
	return string(category)
}

// AvailabilityRequest preserves the authenticated HTTP request shape while the
// Scheduling module owns its behavior.
type AvailabilityRequest = schedulingmodule.SearchCommand

// HandleGetAvailability searches through the Scheduling module.
func (h *Handlers) HandleGetAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req AvailabilityRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	if h.scheduling == nil {
		recordRequestOutcome(r.Context(), outcomeInternalFailure, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Appointment scheduling is temporarily unavailable. Please try again.",
		})
		return
	}
	response, err := h.scheduling.Search(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: err.Error()})
		return
	}
	if response.Status == domain.AvailabilityStatusError {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, safeerrors.CategoryInvalidResponse)
	}
	json.NewEncoder(w).Encode(response)
}

// HandleListAppointmentSlots loads an inventory window through the Scheduling module.
func (h *Handlers) HandleListAppointmentSlots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req schedulingmodule.ListCommand
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	if h.scheduling == nil {
		recordRequestOutcome(r.Context(), outcomeInternalFailure, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Appointment scheduling is temporarily unavailable. Please try again.",
		})
		return
	}
	response, err := h.scheduling.List(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: err.Error()})
		return
	}
	if response.Status == domain.AvailabilityStatusError {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, safeerrors.CategoryInvalidResponse)
	}
	json.NewEncoder(w).Encode(response)
}

// UpdateInsuranceRequest is the expected JSON body for insurance updates.
type UpdateInsuranceRequest struct {
	PatientID      string `json:"patientId"`
	DOB            string `json:"dob,omitempty"`
	InsPlanID      string `json:"insPlanId"`
	RespPartyID    string `json:"respPartyId"`
	OldInsurance   string `json:"oldInsurance"`
	Insurance      string `json:"insurance"`
	CoverageType   string `json:"coverageType,omitempty"`
	SubscriberName string `json:"subscriberName"`
	SubscriberNum  string `json:"subscriberNum"`
	Office         string `json:"office,omitempty"`
}

// UpdateInsuranceResponse is returned after updating insurance.
type UpdateInsuranceResponse struct {
	Status           string   `json:"status"`
	Outcome          string   `json:"outcome,omitempty"`
	PatientID        string   `json:"patientId,omitempty"`
	OldInsurance     string   `json:"oldInsurance,omitempty"`
	NewInsurance     string   `json:"newInsurance,omitempty"`
	Routing          string   `json:"routing,omitempty"`
	AllowedProviders []string `json:"allowedProviders,omitempty"`
	RoutingAmbiguous bool     `json:"routingAmbiguous,omitempty"`
	PreauthRequired  bool     `json:"preauthRequired,omitempty"`
	Message          string   `json:"message,omitempty"`
}

// HandleUpdateInsurance swaps a patient's insurance: end-dates the old plan and attaches a new one.
func (h *Handlers) HandleUpdateInsurance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req UpdateInsuranceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	result := h.patient.UpdateInsurance(r.Context(), patientmodule.UpdateInsuranceCommand{
		PatientID:      req.PatientID,
		DOB:            req.DOB,
		InsPlanID:      req.InsPlanID,
		RespPartyID:    req.RespPartyID,
		OldInsurance:   req.OldInsurance,
		Insurance:      req.Insurance,
		CoverageType:   req.CoverageType,
		SubscriberName: req.SubscriberName,
		SubscriberNum:  req.SubscriberNum,
		Office:         req.Office,
	})
	recordPatientMutationOutcome(r.Context(), result.Outcome)
	outcome := ""
	if result.Status != patientmodule.UpdateInsuranceStatusUpdated {
		outcome = string(result.Outcome)
	}
	json.NewEncoder(w).Encode(UpdateInsuranceResponse{
		Status:           string(result.Status),
		Outcome:          outcome,
		PatientID:        result.PatientID,
		OldInsurance:     result.OldInsurance,
		NewInsurance:     result.NewInsurance,
		Routing:          string(result.Routing),
		AllowedProviders: result.AllowedProviders,
		RoutingAmbiguous: result.RoutingAmbiguous,
		PreauthRequired:  result.PreauthRequired,
		Message:          result.Message,
	})
}

func recordPatientMutationOutcome(ctx context.Context, outcome patientmodule.MutationOutcome) {
	switch outcome {
	case "", patientmodule.MutationReconciledSuccess:
		return
	case patientmodule.MutationValidationFailed:
		recordRequestOutcome(ctx, outcomeInvalidRequest, safeerrors.CategoryNone)
	case patientmodule.MutationRejected:
		recordRequestOutcome(ctx, outcomeProviderFailure, safeerrors.CategoryRejected)
	case patientmodule.MutationUnavailable:
		recordRequestOutcome(ctx, outcomeProviderFailure, safeerrors.CategoryUnavailable)
	default:
		recordRequestOutcome(ctx, outcomeProviderFailure, safeerrors.CategoryUpstreamError)
	}
}

func recordSchedulingError(ctx context.Context, err error) {
	providerFailure := schedulingmodule.ProviderFailureOf(err)
	if providerFailure != safeerrors.CategoryNone {
		recordRequestOutcome(ctx, outcomeProviderFailure, providerFailure)
		return
	}
	switch schedulingmodule.CategoryOf(err) {
	case schedulingmodule.CategoryProviderConflict,
		schedulingmodule.CategoryProviderRejected,
		schedulingmodule.CategoryWriteFailed,
		schedulingmodule.CategoryIndeterminateWrite:
		recordRequestOutcome(ctx, outcomeProviderFailure, safeerrors.CategoryUpstreamError)
	default:
		recordRequestOutcome(ctx, outcomeInvalidRequest, safeerrors.CategoryNone)
	}
}
