package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/domain"
	patientmodule "advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

type errorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Handlers struct {
	session    session.Session
	patient    patientmodule.Patient
	scheduling schedulingmodule.Scheduling
}

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

func (h *Handlers) HandleLive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handlers) HandleReady(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ready"}`))
}

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

func (h *Handlers) HandleAddPatient(w http.ResponseWriter, r *http.Request) {
	var command patientmodule.CreateCommand
	if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
		rejectInvalidRequest(w, r, patientmodule.CreateResult{Status: patientmodule.CreateStatusError, Message: "Invalid JSON body"})
		return
	}

	result := h.patient.Create(r.Context(), command)
	recordPatientMutationOutcome(r.Context(), result.Outcome)
	if result.Status == patientmodule.CreateStatusCreated {
		result.Outcome = ""
	}
	respond(w, result)
}

func (h *Handlers) HandlePatientResolve(w http.ResponseWriter, r *http.Request) {
	var command patientmodule.ResolveCommand
	if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
		rejectInvalidRequest(w, r, patientmodule.ResolveResult{Status: "error", Message: "Invalid JSON body"})
		return
	}

	office, err := domain.ResolveOffice(command.Office)
	if err != nil {
		rejectInvalidRequest(w, r, patientmodule.ResolveResult{Status: "error", Message: err.Error()})
		return
	}
	if message := command.Validate(); message != "" {
		rejectInvalidRequest(w, r, patientmodule.ResolveResult{Status: "error", Message: message})
		return
	}
	command.OfficeID = office.ID

	result, err := h.patient.Resolve(r.Context(), command)
	recordPatientResolutionObservation(r.Context(), result.Observation)
	if err != nil {
		category := advancedmd.CategoryOf(err)
		recordRequestOutcome(r.Context(), outcomeProviderFailure, category)
		message := "Failed to look up patient in AdvancedMD. Please try again."
		if category == safeerrors.CategoryAuthentication || category == safeerrors.CategoryUnavailable {
			message = "Service authentication is temporarily unavailable. Please try again."
		}
		respond(w, patientmodule.ResolveResult{Status: "error", Message: message})
		return
	}
	if result.ProviderFailure != "" && result.ProviderFailure != safeerrors.CategoryNone {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, result.ProviderFailure)
	}
	if result.Appointments == nil {
		result.Appointments = []patientmodule.Appointment{}
	}
	if result.Matches == nil {
		result.Matches = []patientmodule.Candidate{}
	}
	respond(w, result)
}

type cancelAppointmentRequest struct {
	AppointmentID     int    `json:"appointmentId,omitempty"`
	PatientID         string `json:"patientId,omitempty"`
	Office            string `json:"office,omitempty"`
	CancellationToken string `json:"cancellationToken,omitempty"`

	cancellationTokenProvided bool
}

func (r *cancelAppointmentRequest) UnmarshalJSON(data []byte) error {
	type request cancelAppointmentRequest
	var decoded request
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = cancelAppointmentRequest(decoded)
	_, r.cancellationTokenProvided = fields["cancellationToken"]
	return nil
}

func (r cancelAppointmentRequest) command() schedulingmodule.CancelCommand {
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

func (h *Handlers) HandleCancelAppointment(w http.ResponseWriter, r *http.Request) {
	var req cancelAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rejectInvalidRequest(w, r, schedulingmodule.CancelReceipt{
			Status:  "error",
			Message: "Invalid JSON body",
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
		respond(w, schedulingmodule.CancelReceipt{
			Status:  "error",
			Outcome: schedulingOutcome(err),
			Message: err.Error(),
		})
		return
	}
	respond(w, response)
}

func (h *Handlers) HandleBookAppointment(w http.ResponseWriter, r *http.Request) {
	var req schedulingmodule.BookCommand
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rejectInvalidRequest(w, r, schedulingmodule.BookReceipt{Status: "error", Message: "Invalid JSON body"})
		return
	}
	response, err := h.scheduling.Book(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		respond(w, schedulingmodule.BookReceipt{
			Status:  "error",
			Outcome: schedulingOutcome(err),
			Message: err.Error(),
			Missing: schedulingmodule.MissingOf(err),
		})
		return
	}
	respond(w, response)
}

func schedulingOutcome(err error) string {
	category := schedulingmodule.CategoryOf(err)
	if category == schedulingmodule.CategoryValidation {
		return ""
	}
	return string(category)
}

func (h *Handlers) HandleGetAvailability(w http.ResponseWriter, r *http.Request) {
	var req schedulingmodule.SearchCommand
	if err := decodeStrict(r, &req); err != nil {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	response, err := h.scheduling.Search(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		respond(w, errorResponse{Status: "error", Message: err.Error()})
		return
	}
	if response.Status == schedulingmodule.AvailabilityStatusError {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, safeerrors.CategoryInvalidResponse)
	}
	respond(w, response)
}

func (h *Handlers) HandleListAppointmentSlots(w http.ResponseWriter, r *http.Request) {
	var req schedulingmodule.ListCommand
	if err := decodeStrict(r, &req); err != nil {
		rejectInvalidRequest(w, r, inventoryError(schedulingmodule.AvailabilityOutcomeInvalidInput, "Invalid JSON body"))
		return
	}

	response, err := h.scheduling.List(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		outcome := schedulingmodule.AvailabilityOutcomeInvalidInput
		if schedulingmodule.ProviderFailureOf(err) != safeerrors.CategoryNone {
			outcome = schedulingmodule.AvailabilityOutcomeSearchIncomplete
		}
		respond(w, inventoryError(outcome, err.Error()))
		return
	}
	if response.Status == schedulingmodule.AvailabilityStatusError {
		response.NextAction = ""
		recordRequestOutcome(r.Context(), outcomeProviderFailure, safeerrors.CategoryInvalidResponse)
	}
	respond(w, response)
}

func inventoryError(outcome, message string) schedulingmodule.AvailabilityResponse {
	return schedulingmodule.AvailabilityResponse{
		Status: schedulingmodule.AvailabilityStatusError, Outcome: outcome,
		ShouldRetrySameSearch: outcome == schedulingmodule.AvailabilityOutcomeSearchIncomplete,
		Message:               message, Slots: []schedulingmodule.AvailabilitySlotOption{},
	}
}

func (h *Handlers) HandleUpdateInsurance(w http.ResponseWriter, r *http.Request) {
	var command patientmodule.UpdateInsuranceCommand
	if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
		rejectInvalidRequest(w, r, patientmodule.UpdateInsuranceResult{Effect: "no_effect", Status: patientmodule.UpdateInsuranceStatusError, Message: "Invalid JSON body"})
		return
	}

	result := h.patient.UpdateInsurance(r.Context(), command)
	recordPatientMutationOutcome(r.Context(), result.Outcome)
	if result.Status == patientmodule.UpdateInsuranceStatusUpdated {
		result.Outcome = ""
	}
	respond(w, result)
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

func (h *Handlers) HandleRescheduleAppointment(w http.ResponseWriter, r *http.Request) {
	var req schedulingmodule.BookCommand
	if err := decodeStrict(r, &req); err != nil {
		rejectInvalidRequest(w, r, schedulingmodule.RescheduleReceipt{Status: "failed", Message: "Invalid JSON body"})
		return
	}
	receipt, err := h.scheduling.Reschedule(r.Context(), req)
	if err != nil {
		recordSchedulingError(r.Context(), err)
		receipt = schedulingmodule.RescheduleReceipt{Status: "failed", Outcome: schedulingOutcome(err), Message: err.Error()}
	}
	if err == nil && receipt.Status != "completed" {
		if receipt.Failure != nil {
			recordSchedulingError(r.Context(), receipt.Failure)
		} else {
			recordRequestOutcome(r.Context(), outcomeCategory("reschedule_"+receipt.Status), safeerrors.CategoryNone)
		}
	}
	respond(w, receipt)
}
