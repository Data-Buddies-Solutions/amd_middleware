package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/auth"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// eastern is the America/New_York timezone, loaded once at startup.
var eastern *time.Location

func init() {
	var err error
	eastern, err = time.LoadLocation("America/New_York")
	if err != nil {
		eastern = time.FixedZone("EST", -5*3600)
	}
}

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
	Status              string                   `json:"status"`
	PatientID           string                   `json:"patientId,omitempty"`
	Name                string                   `json:"name,omitempty"`
	DOB                 string                   `json:"dob,omitempty"`
	Phone               string                   `json:"phone,omitempty"`
	InsuranceCarrier    string                   `json:"insuranceCarrier,omitempty"`
	InsuranceCarrierID  string                   `json:"insuranceCarrierId,omitempty"`
	InsPlanID           string                   `json:"insPlanId,omitempty"`
	RespPartyID         string                   `json:"respPartyId,omitempty"`
	Routing             string                   `json:"routing,omitempty"`
	AllowedProviders    []string                 `json:"allowedProviders,omitempty"`
	RoutingAmbiguous    bool                     `json:"routingAmbiguous,omitempty"`
	PreauthRequired     bool                     `json:"preauthRequired,omitempty"`
	AppointmentsStatus  string                   `json:"appointmentsStatus,omitempty"`
	Appointments        []PatientApptDetail      `json:"appointments"`
	AppointmentsMessage string                   `json:"appointmentsMessage,omitempty"`
	Message             string                   `json:"message,omitempty"`
	Matches             []PatientResolveResponse `json:"matches,omitempty"`
}

const (
	appointmentsStatusFound = "found"
	appointmentsStatusNone  = "none"
	appointmentsStatusError = "error"
)

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	tokenManager       *auth.TokenManager
	amdClient          *clients.AdvancedMDClient
	amdRestClient      *clients.AdvancedMDRestClient
	schedulingWorkflow *schedulingWorkflow
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(tm *auth.TokenManager, amdClient *clients.AdvancedMDClient, amdRestClient *clients.AdvancedMDRestClient, bookingTokenSecret ...string) *Handlers {
	secret := ""
	if len(bookingTokenSecret) > 0 {
		secret = bookingTokenSecret[0]
	}
	handlers := &Handlers{
		tokenManager:  tm,
		amdClient:     amdClient,
		amdRestClient: amdRestClient,
	}
	handlers.schedulingWorkflow = newSchedulingWorkflow(tm, amdClient, amdRestClient, secret)
	return handlers
}

// SetAllowRawSlotBooking enables the legacy raw scheduler field booking path.
func (h *Handlers) SetAllowRawSlotBooking(allow bool) {
	h.workflow().allowRawBooking = allow
}

func (h *Handlers) workflow() *schedulingWorkflow {
	if h.schedulingWorkflow == nil {
		h.schedulingWorkflow = newSchedulingWorkflow(h.tokenManager, h.amdClient, h.amdRestClient, "")
	}
	return h.schedulingWorkflow
}

// resolveOffice resolves an office name to its config.
// Empty name defaults to Spring Hill for backward compatibility.
func resolveOffice(officeName string) (*domain.OfficeConfig, error) {
	if officeName == "" {
		return domain.DefaultOffice(), nil
	}
	office, ok := domain.LookupOffice(officeName)
	if !ok {
		return nil, fmt.Errorf("unknown office: %q. Valid options: %s", officeName, strings.Join(domain.ValidOfficeNames(), ", "))
	}
	return office, nil
}

func validateOptionalDOB(dob string) error {
	if dob == "" {
		return nil
	}
	if _, ok := domain.AgeYears(dob); !ok {
		return fmt.Errorf("dob must be a valid date")
	}
	return nil
}

// HandleHealth returns a simple health check response.
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
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
		log.Printf("add-patient: failed to decode JSON")
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}
	policy := domain.NewSchedulingPolicy(office)

	insuranceMode := domain.InsuranceModeForCoverage(req.CoverageType)
	if domain.IsSelfPayInsurance(req.Insurance) && strings.TrimSpace(req.SubscriberNum) == "" {
		req.SubscriberNum = "self pay"
	}

	log.Printf("add-patient: received request office=%s coverageMode=%s", office.ID, insuranceMode)

	// Validate required fields (aptSuite and email are optional)
	missing := addPatientMissingFields(req)
	if len(missing) > 0 {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: fmt.Sprintf("Missing required fields: %s", strings.Join(missing, ", ")),
		})
		return
	}
	if insuranceMode == domain.InsuranceModeVision && !policy.SupportsRouting(domain.RoutingOpticalOnly) {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: fmt.Sprintf("Routine vision coverage is not supported at %s. Route the patient to Spring Hill routine vision scheduling.", office.DisplayName),
		})
		return
	}
	if insuranceMode == domain.InsuranceModeMedical && !policy.SupportsMedical() {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: fmt.Sprintf("Medical coverage is not supported at %s. Use routine vision coverage for this office or route medical visits to a medical office.", office.DisplayName),
		})
		return
	}

	// Normalize inputs
	normalizedDOB := domain.NormalizeDOB(req.DOB)
	formattedPhone := domain.FormatPhone(req.Phone)
	normalizedSex := domain.NormalizeSex(req.Sex)
	normalizedFirstName := domain.StripDiacritics(req.FirstName)
	normalizedLastName := domain.StripDiacritics(req.LastName)
	normalizedEmail := strings.TrimSpace(req.Email)

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		log.Printf("add-patient: authentication failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: "Service authentication is temporarily unavailable. Please try again.",
		})
		return
	}

	// Create patient in AMD
	rawPatientID, respPartyID, patientName, err := h.amdClient.AddPatient(r.Context(), tokenData, clients.AddPatientParams{
		FirstName: normalizedFirstName,
		LastName:  normalizedLastName,
		DOB:       normalizedDOB,
		Phone:     formattedPhone,
		Email:     normalizedEmail,
		Street:    req.Street,
		AptSuite:  req.AptSuite,
		City:      req.City,
		State:     strings.ToUpper(req.State),
		Zip:       req.Zip,
		Sex:       normalizedSex,
		SSN:       strings.TrimSpace(req.SSN),
		ProfileID: office.DefaultProfileID,
	})
	if err != nil {
		log.Printf("add-patient: provider request failed category=%s", safeerrors.Classify(err))
		if strings.Contains(err.Error(), "Duplicate name/DOB") {
			json.NewEncoder(w).Encode(AddPatientResponse{
				Status:  "error",
				Message: "A patient with this name and date of birth already exists in the system. Please try verifying the patient again instead of registering.",
			})
			return
		}
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: "Failed to create patient in AdvancedMD. Please try again or contact the office.",
		})
		return
	}

	strippedID := domain.StripPatientPrefix(rawPatientID)

	// Look up insurance entry from name
	insEntry, ok := domain.LookupInsuranceForCoverageAtOffice(req.Insurance, insuranceMode, office)
	if !ok {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:    "partial",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
			Message:   fmt.Sprintf("Patient created but insurance not recognized: %q. Please use an insurance name from the accepted list.", req.Insurance),
		})
		return
	}

	// Reject insurance not accepted at this office
	if insEntry.Routing == domain.RoutingNotAccepted {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:    "partial",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
			Message:   fmt.Sprintf("%s is not accepted at %s. The patient may self-pay or contact the office for options.", req.Insurance, office.DisplayName),
		})
		return
	}

	// Attach insurance
	if err := h.amdClient.AddInsurance(r.Context(), tokenData, rawPatientID, respPartyID, insEntry.CarrierID, req.SubscriberNum); err != nil {
		log.Printf("add-patient: add insurance failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:    "partial",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
			Message:   "Patient created but insurance could not be attached. Please contact the office.",
		})
		return
	}

	routing := insEntry.Routing
	if insuranceMode == domain.InsuranceModeMedical {
		routing = policy.SchedulingRouting(routing, normalizedDOB)
	}

	json.NewEncoder(w).Encode(AddPatientResponse{
		Status:           "created",
		PatientID:        strippedID,
		Name:             patientName,
		DOB:              normalizedDOB,
		Routing:          string(routing),
		AllowedProviders: policy.ProviderNames(routing, normalizedDOB),
		PreauthRequired:  insEntry.PreauthRequired,
		Message:          "Patient created and insurance attached successfully",
	})
}

func addPatientMissingFields(req AddPatientRequest) []string {
	missing := []string{}
	if req.FirstName == "" {
		missing = append(missing, "firstName")
	}
	if req.LastName == "" {
		missing = append(missing, "lastName")
	}
	if req.DOB == "" {
		missing = append(missing, "dob")
	}
	if req.Phone == "" {
		missing = append(missing, "phone")
	}
	if req.Street == "" {
		missing = append(missing, "street")
	}
	if req.City == "" {
		missing = append(missing, "city")
	}
	if req.State == "" {
		missing = append(missing, "state")
	}
	if req.Zip == "" {
		missing = append(missing, "zip")
	}
	if req.Sex == "" {
		missing = append(missing, "sex")
	}
	if req.Insurance == "" {
		missing = append(missing, "insurance")
	}
	if req.SubscriberName == "" {
		missing = append(missing, "subscriberName")
	}
	if req.SubscriberNum == "" {
		missing = append(missing, "subscriberNum")
	}
	return missing
}

// PatientApptDetail is a single appointment formatted for LLM consumption.
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
}

// HandlePatientResolve resolves a patient and, by default, loads appointments.
func (h *Handlers) HandlePatientResolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req PatientResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	if msg := validatePatientResolveRequest(req); msg != "" {
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: msg,
		})
		return
	}

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		log.Printf("patient-resolve: authentication failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: "Service authentication is temporarily unavailable. Please try again.",
		})
		return
	}

	if req.PatientID != "" {
		resp := h.resolveKnownPatient(r.Context(), tokenData, req, office)
		json.NewEncoder(w).Encode(resp)
		return
	}

	patients, err := h.resolvePatientCandidates(r.Context(), tokenData, req)
	if err != nil {
		log.Printf("patient-resolve: lookup failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: "Failed to look up patient in AdvancedMD. Please try again.",
		})
		return
	}

	patient, matchingPatients := selectResolvedPatient(patients, req)
	if patient.ID == "" {
		if len(matchingPatients) == 0 {
			json.NewEncoder(w).Encode(PatientResolveResponse{
				Status:       "not_found",
				Appointments: []PatientApptDetail{},
				Message:      notFoundMessage(req),
			})
			return
		}
		matches := h.buildResolvedPatientMatches(r.Context(), tokenData, matchingPatients, office, req.Phone)
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:       "multiple_matches",
			Appointments: []PatientApptDetail{},
			Matches:      matches,
			Message:      multipleMatchesMessage(req, len(matches)),
		})
		return
	}

	resp := h.buildResolvedPatient(r.Context(), tokenData, patient, office, req.Phone)
	json.NewEncoder(w).Encode(resp)
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
		return ""
	}
	if req.LastName != "" && req.DOB != "" {
		return ""
	}
	return "Provide patientId, phone, phone + firstName, phone + dob, or lastName + dob"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (h *Handlers) resolvePatientCandidates(ctx context.Context, tokenData *domain.TokenData, req PatientResolveRequest) ([]domain.Patient, error) {
	if req.Phone != "" {
		digits := domain.NormalizePhoneDigits(req.Phone)
		patients, err := h.amdClient.LookupPatientByPhone(ctx, tokenData, digits)
		if err != nil {
			return nil, fmt.Errorf("Failed to lookup patient by phone: %w", err)
		}
		log.Printf("patient-resolve: phone lookup returned %d patients", len(patients))
		return patients, nil
	}

	normalizedLastName := domain.StripDiacritics(req.LastName)
	normalizedFirstName := domain.StripDiacritics(req.FirstName)
	patients, err := h.amdClient.LookupPatient(ctx, tokenData, normalizedLastName, normalizedFirstName)
	if err != nil {
		return nil, fmt.Errorf("Failed to lookup patient: %w", err)
	}
	log.Printf("patient-resolve: name lookup returned %d patients", len(patients))
	return patients, nil
}

func selectResolvedPatient(patients []domain.Patient, req PatientResolveRequest) (domain.Patient, []domain.Patient) {
	matching := patients
	if req.DOB != "" {
		normalizedDOB := domain.NormalizeDOB(req.DOB)
		matching = nil
		for _, p := range patients {
			if domain.NormalizeDOB(p.DOB) == normalizedDOB {
				matching = append(matching, p)
			}
		}
	}

	if req.FirstName != "" {
		firstNameMatches := make([]domain.Patient, 0, len(matching))
		for _, p := range matching {
			if patientFirstNameMatches(p, req.FirstName) {
				firstNameMatches = append(firstNameMatches, p)
			}
		}
		matching = firstNameMatches
	}

	switch len(matching) {
	case 0:
		return domain.Patient{}, nil
	case 1:
		return matching[0], nil
	default:
		return domain.Patient{}, matching
	}
}

func patientFirstNameMatches(patient domain.Patient, firstName string) bool {
	patientFirst := strings.ToUpper(domain.StripDiacritics(patient.FirstName))
	requestFirst := strings.ToUpper(domain.StripDiacritics(firstName))
	return strings.HasPrefix(patientFirst, requestFirst)
}

func notFoundMessage(req PatientResolveRequest) string {
	if req.Phone != "" && req.FirstName == "" && req.DOB == "" {
		return "No patient found for that phone number"
	}
	if req.FirstName != "" {
		return "No patient found matching that first name"
	}
	return "No patient found matching the provided information"
}

func multipleMatchesMessage(req PatientResolveRequest, count int) string {
	if req.Phone != "" && req.FirstName != "" && req.DOB == "" {
		return fmt.Sprintf("Found %d patients with that name and phone number. Please provide date of birth.", count)
	}
	if req.Phone != "" {
		return fmt.Sprintf("Found %d patients for this phone number. Ask the caller to confirm their name.", count)
	}
	return fmt.Sprintf("Found %d patients with that last name and DOB. Please provide first name.", count)
}

func (h *Handlers) resolveKnownPatient(ctx context.Context, tokenData *domain.TokenData, req PatientResolveRequest, office *domain.OfficeConfig) PatientResolveResponse {
	resp := PatientResolveResponse{
		Status:       "verified",
		PatientID:    req.PatientID,
		Appointments: []PatientApptDetail{},
	}

	demoResult, err := h.amdClient.GetDemographic(ctx, tokenData, req.PatientID)
	if err != nil {
		log.Printf("WARNING: patient-resolve: failed to get demographics category=%s", safeerrors.Classify(err))
	} else if demoResult != nil {
		applyDemographicsToResolveResponse(&resp, demoResult, office, demoResult.DOB)
		resp.DOB = demoResult.DOB
	}

	attachAppointments(ctx, h, tokenData, &resp, req.PatientID, office)
	setPatientResolveMessage(&resp)
	return resp
}

func (h *Handlers) buildResolvedPatient(ctx context.Context, tokenData *domain.TokenData, patient domain.Patient, office *domain.OfficeConfig, lookupPhone string) PatientResolveResponse {
	resp := PatientResolveResponse{
		Status:       "verified",
		PatientID:    patient.ID,
		Name:         patient.FullName,
		DOB:          patient.DOB,
		Phone:        firstNonEmpty(patient.Phone, lookupPhone),
		Appointments: []PatientApptDetail{},
	}

	demoResult, err := h.amdClient.GetDemographic(ctx, tokenData, patient.ID)
	if err != nil {
		log.Printf("WARNING: patient-resolve: failed to get demographics category=%s", safeerrors.Classify(err))
	} else if demoResult != nil {
		applyDemographicsToResolveResponse(&resp, demoResult, office, patient.DOB)
	}

	attachAppointments(ctx, h, tokenData, &resp, patient.ID, office)
	setPatientResolveMessage(&resp)
	return resp
}

func (h *Handlers) buildResolvedPatientMatches(ctx context.Context, tokenData *domain.TokenData, patients []domain.Patient, office *domain.OfficeConfig, lookupPhone string) []PatientResolveResponse {
	matches := make([]PatientResolveResponse, 0, len(patients))
	for _, patient := range patients {
		matches = append(matches, h.buildResolvedPatient(ctx, tokenData, patient, office, lookupPhone))
	}
	return matches
}

func applyDemographicsToResolveResponse(resp *PatientResolveResponse, demoResult *clients.DemographicResult, office *domain.OfficeConfig, patientDOB string) {
	policy := domain.NewSchedulingPolicy(office)
	resp.InsuranceCarrier = demoResult.CarrierName
	resp.InsPlanID = demoResult.InsPlanID
	resp.RespPartyID = demoResult.RespPartyID

	if demoResult.CarrierID != "" {
		resp.InsuranceCarrierID = demoResult.CarrierID
		routing, ambiguous := domain.RoutingForDemographicInsurance(demoResult.CarrierID, demoResult.CarrierName, office)
		routing = policy.PatientRouting(routing, patientDOB)
		resp.Routing = string(routing)
		resp.AllowedProviders = policy.ProviderNames(routing, patientDOB)
		resp.RoutingAmbiguous = ambiguous
		if entry, ok := domain.LookupInsuranceForCoverageAtOffice(demoResult.CarrierName, domain.InsuranceModeMedical, office); ok {
			resp.PreauthRequired = entry.PreauthRequired
		}
		if domain.IsMinor(patientDOB) && routing != domain.RoutingNotAccepted {
			resp.RoutingAmbiguous = false
		}
	}
}

func attachAppointments(ctx context.Context, h *Handlers, tokenData *domain.TokenData, resp *PatientResolveResponse, patientID string, office *domain.OfficeConfig) {
	if h.amdRestClient == nil {
		resp.AppointmentsStatus = appointmentsStatusError
		resp.Appointments = []PatientApptDetail{}
		resp.AppointmentsMessage = "AdvancedMD appointment client is not configured"
		return
	}

	appts, err := h.fetchUpcomingAppointments(ctx, tokenData, patientID, office)
	if err != nil {
		log.Printf("WARNING: patient-resolve: failed to get appointments category=%s", safeerrors.Classify(err))
		resp.AppointmentsStatus = appointmentsStatusError
		resp.Appointments = []PatientApptDetail{}
		resp.AppointmentsMessage = "Failed to retrieve appointments from AdvancedMD. Please try again."
		return
	}

	resp.Appointments = appts
	if len(appts) > 0 {
		resp.AppointmentsStatus = appointmentsStatusFound
	} else {
		resp.AppointmentsStatus = appointmentsStatusNone
	}
}

func setPatientResolveMessage(resp *PatientResolveResponse) {
	switch resp.AppointmentsStatus {
	case appointmentsStatusFound:
		resp.Message = fmt.Sprintf("Patient verified with %d appointment(s)", len(resp.Appointments))
	case appointmentsStatusNone:
		resp.Message = "Patient verified, no appointments found"
	case appointmentsStatusError:
		resp.Message = "Patient verified, appointment lookup unavailable"
	default:
		resp.Message = "Patient verified"
	}
}

// fetchUpcomingAppointments retrieves future appointments for a patient ID
// (current month + 5 months forward).
func (h *Handlers) fetchUpcomingAppointments(ctx context.Context, tokenData *domain.TokenData, patientID string, office *domain.OfficeConfig) ([]PatientApptDetail, error) {
	lookupOffices := domain.AppointmentLookupOffices(office)
	details := make([]PatientApptDetail, 0)
	for _, lookupOffice := range lookupOffices {
		officeDetails, err := h.fetchUpcomingAppointmentsForOffice(ctx, tokenData, patientID, lookupOffice)
		if err != nil {
			return nil, err
		}
		details = append(details, officeDetails...)
	}
	return details, nil
}

func (h *Handlers) fetchUpcomingAppointmentsForOffice(ctx context.Context, tokenData *domain.TokenData, patientID string, office *domain.OfficeConfig) ([]PatientApptDetail, error) {
	patientIDInt, err := strconv.Atoi(patientID)
	if err != nil {
		return nil, fmt.Errorf("patientId must be numeric: %w", err)
	}

	columnIDStr := strings.Join(office.AllowedColumnIDs(), "-")

	now := time.Now().In(eastern)
	cutoff := appointmentLookupCutoff(now)
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, eastern)

	months := make([]time.Time, 6)
	for i := range months {
		months[i] = thisMonth.AddDate(0, i, 0)
	}

	type monthResult struct {
		appts []clients.AMDAppointmentResponse
		err   error
	}
	ch := make(chan monthResult, len(months))

	for _, m := range months {
		m := m
		go func() {
			appts, err := h.amdRestClient.GetAppointmentsByMonth(ctx, tokenData, columnIDStr, m.Format("2006-01-02"))
			ch <- monthResult{appts, err}
		}()
	}

	var allAppts []clients.AMDAppointmentResponse
	for range months {
		r := <-ch
		if r.err != nil {
			return nil, r.err
		}
		allAppts = append(allAppts, r.appts...)
	}
	var details []PatientApptDetail
	for _, a := range allAppts {
		if a.PatientID != patientIDInt {
			continue
		}

		startTime, err := clients.ParseDateTime(a.StartDateTime)
		if err != nil {
			continue
		}
		if !startTime.After(cutoff) {
			continue
		}

		appointmentTypeID := 0
		typeName := ""
		if len(a.AppointmentTypes) > 0 {
			appointmentTypeID = a.AppointmentTypes[0]
			if canonicalTypeID, ok := domain.CanonicalAppointmentTypeID(appointmentTypeID); ok {
				appointmentTypeID = canonicalTypeID
			}
			if name, ok := office.AppointmentTypeName(appointmentTypeID); ok {
				typeName = name
			}
		}

		detail := PatientApptDetail{
			ID:                a.ID,
			Date:              startTime.Format("Monday, January 2, 2006"),
			Time:              startTime.Format("3:04 PM"),
			Provider:          office.FriendlyProviderName(a.Provider),
			Type:              typeName,
			AppointmentTypeID: appointmentTypeID,
			Facility:          appointmentFacilityName(a.Facility, office),
			OfficeID:          office.ID,
			Office:            office.DisplayName,
		}
		details = append(details, detail)
	}

	return details, nil
}

func appointmentLookupCutoff(now time.Time) time.Time {
	local := now.In(eastern)
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC)
}

// friendlyFacilityName cleans up AMD facility names to title case.
func friendlyFacilityName(amdName string) string {
	if amdName == "" {
		return ""
	}
	return cases.Title(language.English).String(strings.ToLower(amdName))
}

func appointmentFacilityName(amdName string, office *domain.OfficeConfig) string {
	facility := friendlyFacilityName(amdName)
	if facility != "" {
		return facility
	}
	return office.DisplayName
}

// CancelAppointmentRequest is the expected JSON body for cancelling an appointment.
type CancelAppointmentRequest struct {
	AppointmentID int    `json:"appointmentId"`
	PatientID     string `json:"patientId,omitempty"`
	Office        string `json:"office,omitempty"`
}

// CancelAppointmentResponse is returned after cancelling an appointment.
type CancelAppointmentResponse struct {
	Status        string `json:"status"`
	AppointmentID int    `json:"appointmentId,omitempty"`
	Message       string `json:"message"`
}

// HandleCancelAppointment cancels an appointment in AdvancedMD.
func (h *Handlers) HandleCancelAppointment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req CancelAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	if req.AppointmentID == 0 {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "appointmentId is required",
		})
		return
	}
	req.PatientID = domain.StripPatientPrefix(strings.TrimSpace(req.PatientID))
	if req.PatientID == "" {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "patientId is required",
		})
		return
	}
	if _, err := strconv.Atoi(req.PatientID); err != nil {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "patientId must be numeric",
		})
		return
	}
	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		log.Printf("cancel-appointment: authentication failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Service authentication is temporarily unavailable. Please try again.",
		})
		return
	}

	owningOffice, err := h.cancelableAppointmentOffice(r.Context(), tokenData, req.PatientID, req.AppointmentID, office)
	if err != nil {
		log.Printf("cancel-appointment: appointment ownership check failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Unable to verify appointment before cancellation. Please load appointments again and choose the appointment to cancel.",
		})
		return
	}
	if owningOffice == nil {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "No upcoming appointment matches that patient and appointment ID. Please load appointments again and choose the appointment to cancel.",
		})
		return
	}

	log.Printf("cancel-appointment: request office=%s", owningOffice.ID)

	// Cancel via AMD REST API
	if err := h.amdRestClient.CancelAppointment(r.Context(), tokenData, req.AppointmentID); err != nil {
		log.Printf("cancel-appointment: provider request failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Failed to cancel appointment in AdvancedMD. Please try again or contact the office.",
		})
		return
	}

	json.NewEncoder(w).Encode(CancelAppointmentResponse{
		Status:        "cancelled",
		AppointmentID: req.AppointmentID,
		Message:       "Appointment cancelled successfully",
	})
}

func (h *Handlers) cancelableAppointmentOffice(ctx context.Context, tokenData *domain.TokenData, patientID string, appointmentID int, office *domain.OfficeConfig) (*domain.OfficeConfig, error) {
	if h.amdRestClient == nil {
		return nil, fmt.Errorf("AdvancedMD appointment client is not configured")
	}
	appointments, err := h.fetchUpcomingAppointments(ctx, tokenData, patientID, office)
	if err != nil {
		return nil, err
	}
	for _, appointment := range appointments {
		if appointment.ID != appointmentID {
			continue
		}
		if appointment.OfficeID == "" {
			return office, nil
		}
		owningOffice, ok := lookupOfficeByID(appointment.OfficeID)
		if !ok {
			return nil, fmt.Errorf("unknown appointment office ID %q", appointment.OfficeID)
		}
		return owningOffice, nil
	}
	return nil, nil
}

// BookAppointmentRequest is the expected JSON body for booking an appointment.
type BookAppointmentRequest struct {
	PatientID         string `json:"patientId"`
	PatientName       string `json:"patientName,omitempty"`
	DOB               string `json:"dob,omitempty"`
	BookingToken      string `json:"bookingToken,omitempty"`
	ColumnID          int    `json:"columnId"`
	ProfileID         int    `json:"profileId"`
	StartDatetime     string `json:"startDatetime"`
	Duration          int    `json:"duration"`
	AppointmentTypeID int    `json:"appointmentTypeId"`
	Routing           string `json:"routing,omitempty"`
	Office            string `json:"office,omitempty"`
	VisitCategory     string `json:"visitCategory,omitempty"`
	VisitKind         string `json:"visitKind,omitempty"`
	PatientStatus     string `json:"patientStatus,omitempty"`
	AgeBand           string `json:"ageBand,omitempty"`
	IsPostOp          bool   `json:"isPostOp,omitempty"`
	VisitReason       string `json:"visitReason,omitempty"`
	AppointmentReason string `json:"appointmentReason,omitempty"`
	ReferringDoctor   string `json:"referringDoctor,omitempty"`

	bookingRequiresForce      bool
	bookingAppointmentTypeIDs []int
}

// BookAppointmentResponse is returned after booking an appointment.
type BookAppointmentResponse struct {
	Status              string   `json:"status"`
	Outcome             string   `json:"outcome,omitempty"`
	AppointmentID       int      `json:"appointmentId,omitempty"`
	PatientID           string   `json:"patientId,omitempty"`
	PatientName         string   `json:"patientName,omitempty"`
	ProviderName        string   `json:"providerName,omitempty"`
	LocationName        string   `json:"locationName,omitempty"`
	StartDatetime       string   `json:"startDatetime,omitempty"`
	Duration            int      `json:"duration,omitempty"`
	AppointmentTypeID   int      `json:"appointmentTypeId,omitempty"`
	AppointmentTypeName string   `json:"appointmentTypeName,omitempty"`
	Message             string   `json:"message"`
	Missing             []string `json:"missing,omitempty"`
}

// HandleBookAppointment books an appointment through the Scheduling Workflow.
func (h *Handlers) HandleBookAppointment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req BookAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	response, workflowErr := h.workflow().Book(r.Context(), req, time.Now())
	if workflowErr != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Outcome: workflowErr.outcome,
			Message: workflowErr.message,
			Missing: workflowErr.missing,
		})
		return
	}
	json.NewEncoder(w).Encode(response)
}

// AvailabilityRequest is the expected JSON body for availability lookup.
type AvailabilityRequest struct {
	Date            string `json:"date"`            // Required: YYYY-MM-DD format
	Provider        string `json:"provider"`        // Optional: filter by provider name
	Office          string `json:"office"`          // Optional: filter by office name (e.g., "Spring Hill", "Hollywood")
	Routing         string `json:"routing"`         // Optional: routing rule from verify/add-patient (e.g., "bach_only")
	DOB             string `json:"dob,omitempty"`   // Optional: patient DOB for provider age rules
	PreauthRequired bool   `json:"preauthRequired"` // Optional: if true, enforces 14-day minimum lead time
}

// HandleGetAvailability searches through the Scheduling Workflow.
func (h *Handlers) HandleGetAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req AvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	response, workflowErr := h.workflow().Search(r.Context(), req, time.Now())
	if workflowErr != nil {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: workflowErr.message})
		return
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
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	// Validate required fields
	if domain.IsSelfPayInsurance(req.Insurance) && strings.TrimSpace(req.SubscriberNum) == "" {
		req.SubscriberNum = "self pay"
	}
	if req.PatientID == "" || req.Insurance == "" || req.SubscriberNum == "" {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "patientId, insurance, and subscriberNum are required",
		})
		return
	}
	if err := validateOptionalDOB(req.DOB); err != nil {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{Status: "error", Message: err.Error()})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}
	policy := domain.NewSchedulingPolicy(office)
	insuranceMode := domain.InsuranceModeForCoverage(req.CoverageType)
	if insuranceMode == domain.InsuranceModeVision && !policy.SupportsRouting(domain.RoutingOpticalOnly) {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: fmt.Sprintf("Routine vision coverage is not supported at %s. Route the patient to Spring Hill routine vision scheduling.", office.DisplayName),
		})
		return
	}
	if insuranceMode == domain.InsuranceModeMedical && !policy.SupportsMedical() {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: fmt.Sprintf("Medical coverage is not supported at %s. Use routine vision coverage for this office or route medical visits to a medical office.", office.DisplayName),
		})
		return
	}

	// Look up new insurance
	insEntry, found := domain.LookupInsuranceForCoverageAtOffice(req.Insurance, insuranceMode, office)
	if !found {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: fmt.Sprintf("Insurance not recognized: %q. Please use an insurance name from the accepted list.", req.Insurance),
		})
		return
	}

	if insEntry.Routing == domain.RoutingNotAccepted {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: fmt.Sprintf("%s is not accepted at %s.", req.Insurance, office.DisplayName),
		})
		return
	}

	// Get AMD token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		log.Printf("update-insurance: authentication failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "Service authentication is temporarily unavailable. Please try again.",
		})
		return
	}

	// End-date old plan if insplan ID provided
	if req.InsPlanID != "" {
		if err := h.amdClient.EndDateInsurance(r.Context(), tokenData, req.PatientID, req.InsPlanID); err != nil {
			log.Printf("update-insurance: end-date failed category=%s", safeerrors.Classify(err))
			json.NewEncoder(w).Encode(UpdateInsuranceResponse{
				Status:  "error",
				Message: "Failed to update existing insurance in AdvancedMD. Please try again or contact the office.",
			})
			return
		}
	}

	// Add new insurance plan
	if err := h.amdClient.AddInsurance(r.Context(), tokenData, req.PatientID, req.RespPartyID, insEntry.CarrierID, req.SubscriberNum); err != nil {
		log.Printf("update-insurance: add insurance failed category=%s", safeerrors.Classify(err))
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "Failed to attach new insurance in AdvancedMD. Please try again or contact the office.",
		})
		return
	}

	routing := insEntry.Routing
	routing = policy.SchedulingRouting(routing, req.DOB)
	_, ambiguous := domain.RoutingForDemographicInsurance(insEntry.CarrierID, req.Insurance, office)

	json.NewEncoder(w).Encode(UpdateInsuranceResponse{
		Status:           "updated",
		PatientID:        req.PatientID,
		OldInsurance:     req.OldInsurance,
		NewInsurance:     req.Insurance,
		Routing:          string(routing),
		AllowedProviders: policy.ProviderNames(routing, req.DOB),
		RoutingAmbiguous: ambiguous,
		PreauthRequired:  insEntry.PreauthRequired,
		Message:          "Insurance updated successfully",
	})
}
