package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"advancedmd-token-management/internal/auth"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"

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

const maxPatientNoteLength = 1000
const bachSameStartCapacity = 2
const schedulerSetupCacheTTL = 6 * time.Hour

// PatientResolveRequest is the single patient-read request shape. It supports
// pre-call phone lookup, verification by phone/name/DOB, and appointment refresh
// for an already verified patient ID.
type PatientResolveRequest struct {
	PatientID           string `json:"patientId,omitempty"`
	LastName            string `json:"lastName,omitempty"`
	DOB                 string `json:"dob,omitempty"`
	FirstName           string `json:"firstName,omitempty"`
	Phone               string `json:"phone,omitempty"`
	Office              string `json:"office,omitempty"`
	IncludeAppointments *bool  `json:"includeAppointments,omitempty"`
}

// PatientResolveResponse is returned by /api/patient/resolve.
type PatientResolveResponse struct {
	Status              string              `json:"status"`
	PatientID           string              `json:"patientId,omitempty"`
	Name                string              `json:"name,omitempty"`
	DOB                 string              `json:"dob,omitempty"`
	Phone               string              `json:"phone,omitempty"`
	InsuranceCarrier    string              `json:"insuranceCarrier,omitempty"`
	InsuranceCarrierID  string              `json:"insuranceCarrierId,omitempty"`
	InsPlanID           string              `json:"insPlanId,omitempty"`
	RespPartyID         string              `json:"respPartyId,omitempty"`
	Routing             string              `json:"routing,omitempty"`
	AllowedProviders    []string            `json:"allowedProviders,omitempty"`
	RoutingAmbiguous    bool                `json:"routingAmbiguous,omitempty"`
	AppointmentsStatus  string              `json:"appointmentsStatus,omitempty"`
	Appointments        []PatientApptDetail `json:"appointments"`
	AppointmentsMessage string              `json:"appointmentsMessage,omitempty"`
	Message             string              `json:"message,omitempty"`
	Matches             []PatientMatch      `json:"matches,omitempty"`
}

const (
	appointmentsStatusFound   = "found"
	appointmentsStatusNone    = "none"
	appointmentsStatusSkipped = "skipped"
	appointmentsStatusError   = "error"
)

// PatientMatch provides minimal info for disambiguation.
type PatientMatch struct {
	FirstName string `json:"firstName"`
}

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	tokenManager        *auth.TokenManager
	amdClient           *clients.AdvancedMDClient
	amdRestClient       *clients.AdvancedMDRestClient
	bookingTokenSecret  string
	allowRawSlotBooking bool
	allowLegacyCancel   bool

	schedulerSetupMu        sync.Mutex
	schedulerSetup          *domain.SchedulerSetup
	schedulerSetupExpiresAt time.Time
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(tm *auth.TokenManager, amdClient *clients.AdvancedMDClient, amdRestClient *clients.AdvancedMDRestClient, bookingTokenSecret ...string) *Handlers {
	secret := ""
	if len(bookingTokenSecret) > 0 {
		secret = bookingTokenSecret[0]
	}
	return &Handlers{
		tokenManager:       tm,
		amdClient:          amdClient,
		amdRestClient:      amdRestClient,
		bookingTokenSecret: secret,
	}
}

// SetAllowRawSlotBooking enables the legacy raw scheduler field booking path.
func (h *Handlers) SetAllowRawSlotBooking(allow bool) {
	h.allowRawSlotBooking = allow
}

// SetAllowLegacyCancel enables cancellation by appointment ID without a signed cancel token.
func (h *Handlers) SetAllowLegacyCancel(allow bool) {
	h.allowLegacyCancel = allow
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

func effectiveRoutingForDOB(office *domain.OfficeConfig, routing domain.RoutingRule, dob string) domain.RoutingRule {
	if routing == domain.RoutingNotAccepted || routing == domain.RoutingOpticalOnly {
		return routing
	}
	if domain.IsMinor(dob) {
		return office.PediatricRouting
	}
	return routing
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
		log.Printf("add-patient: failed to decode JSON: %v", err)
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

	insuranceMode := domain.InsuranceModeForCoverage(req.CoverageType)
	if domain.IsSelfPayInsurance(req.Insurance) && strings.TrimSpace(req.SubscriberNum) == "" {
		req.SubscriberNum = "self pay"
	}

	log.Printf("add-patient: received request office=%s coverageType=%q", office.ID, req.CoverageType)

	// Validate required fields (aptSuite and email are optional)
	missing := addPatientMissingFields(req)
	if len(missing) > 0 {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: fmt.Sprintf("Missing required fields: %s", strings.Join(missing, ", ")),
		})
		return
	}
	if insuranceMode == domain.InsuranceModeVision && !officeSupportsRouting(office, domain.RoutingOpticalOnly) {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: fmt.Sprintf("Routine vision coverage is not supported at %s. Route the patient to Spring Hill routine vision scheduling.", office.DisplayName),
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
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
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
		ProfileID: office.DefaultProfileID,
	})
	if err != nil {
		log.Printf("add-patient: AMD error: %v", err)
		if strings.Contains(err.Error(), "Duplicate name/DOB") {
			json.NewEncoder(w).Encode(AddPatientResponse{
				Status:  "error",
				Message: "A patient with this name and date of birth already exists in the system. Please try verifying the patient again instead of registering.",
			})
			return
		}
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: "Failed to create patient: " + err.Error(),
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
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:    "partial",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
			Message:   "Patient created but insurance failed: " + err.Error(),
		})
		return
	}

	// Pediatric override: under-18 patients → office pediatric routing
	routing := insEntry.Routing
	if insuranceMode == domain.InsuranceModeMedical && domain.IsMinor(normalizedDOB) && routing != domain.RoutingNotAccepted {
		routing = office.PediatricRouting
	}

	json.NewEncoder(w).Encode(AddPatientResponse{
		Status:           "created",
		PatientID:        strippedID,
		Name:             patientName,
		DOB:              normalizedDOB,
		Routing:          string(routing),
		AllowedProviders: office.ProvidersForRoutingAndDOB(routing, normalizedDOB),
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
	ID          int    `json:"id"`                    // AMD appointment ID — for cancel_appt
	Date        string `json:"date"`                  // Human-readable (e.g., "Wednesday, March 18, 2026")
	Time        string `json:"time"`                  // e.g., "12:00 PM"
	Provider    string `json:"provider,omitempty"`    // e.g., "Dr. Austin Bach"
	Type        string `json:"type,omitempty"`        // e.g., "New Adult Medical"
	Facility    string `json:"facility,omitempty"`    // e.g., "Abita Eye Group Spring Hill"
	CancelToken string `json:"cancelToken,omitempty"` // Signed token binding this appointment to patient and office
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
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
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
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	patient, matches := selectResolvedPatient(patients, req)
	if patient.ID == "" {
		if len(matches) == 0 {
			json.NewEncoder(w).Encode(PatientResolveResponse{
				Status:       "not_found",
				Appointments: []PatientApptDetail{},
				Message:      notFoundMessage(req),
			})
			return
		}
		json.NewEncoder(w).Encode(PatientResolveResponse{
			Status:       "multiple_matches",
			Appointments: []PatientApptDetail{},
			Matches:      matches,
			Message:      multipleMatchesMessage(req, len(matches)),
		})
		return
	}

	resp := h.buildResolvedPatient(r.Context(), tokenData, patient, office, includeAppointments(req))
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

func includeAppointments(req PatientResolveRequest) bool {
	return req.IncludeAppointments == nil || *req.IncludeAppointments
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

func selectResolvedPatient(patients []domain.Patient, req PatientResolveRequest) (domain.Patient, []PatientMatch) {
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
		matches := make([]PatientMatch, 0, len(matching))
		for _, p := range matching {
			matches = append(matches, PatientMatch{FirstName: p.FirstName})
		}
		return domain.Patient{}, matches
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
		log.Printf("WARNING: patient-resolve: failed to get demographics for patientId %s: %v", req.PatientID, err)
	} else if demoResult != nil {
		applyDemographicsToResolveResponse(&resp, demoResult, office, demoResult.DOB)
		resp.DOB = demoResult.DOB
	}

	attachAppointments(ctx, h, tokenData, &resp, req.PatientID, office, includeAppointments(req))
	setPatientResolveMessage(&resp)
	return resp
}

func (h *Handlers) buildResolvedPatient(ctx context.Context, tokenData *domain.TokenData, patient domain.Patient, office *domain.OfficeConfig, loadAppointments bool) PatientResolveResponse {
	resp := PatientResolveResponse{
		Status:       "verified",
		PatientID:    patient.ID,
		Name:         patient.FullName,
		DOB:          patient.DOB,
		Phone:        patient.Phone,
		Appointments: []PatientApptDetail{},
	}

	demoResult, err := h.amdClient.GetDemographic(ctx, tokenData, patient.ID)
	if err != nil {
		log.Printf("WARNING: patient-resolve: failed to get demographics: %v", err)
	} else if demoResult != nil {
		applyDemographicsToResolveResponse(&resp, demoResult, office, patient.DOB)
	}

	attachAppointments(ctx, h, tokenData, &resp, patient.ID, office, loadAppointments)
	setPatientResolveMessage(&resp)
	return resp
}

func applyDemographicsToResolveResponse(resp *PatientResolveResponse, demoResult *clients.DemographicResult, office *domain.OfficeConfig, patientDOB string) {
	resp.InsuranceCarrier = demoResult.CarrierName
	resp.InsPlanID = demoResult.InsPlanID
	resp.RespPartyID = demoResult.RespPartyID

	if demoResult.CarrierID != "" {
		resp.InsuranceCarrierID = demoResult.CarrierID
		routing, ambiguous := domain.RoutingForDemographicInsurance(demoResult.CarrierID, demoResult.CarrierName, office)
		resp.Routing = string(routing)
		resp.AllowedProviders = office.ProvidersForRoutingAndDOB(routing, patientDOB)
		resp.RoutingAmbiguous = ambiguous
	}

	if domain.IsMinor(patientDOB) && resp.Routing != "" && resp.Routing != string(domain.RoutingNotAccepted) {
		resp.Routing = string(office.PediatricRouting)
		resp.AllowedProviders = office.ProvidersForRoutingAndDOB(office.PediatricRouting, patientDOB)
		resp.RoutingAmbiguous = false
	}
}

func attachAppointments(ctx context.Context, h *Handlers, tokenData *domain.TokenData, resp *PatientResolveResponse, patientID string, office *domain.OfficeConfig, loadAppointments bool) {
	if !loadAppointments {
		resp.AppointmentsStatus = appointmentsStatusSkipped
		resp.Appointments = []PatientApptDetail{}
		return
	}
	if h.amdRestClient == nil {
		resp.AppointmentsStatus = appointmentsStatusError
		resp.Appointments = []PatientApptDetail{}
		resp.AppointmentsMessage = "AdvancedMD appointment client is not configured"
		return
	}

	appts, err := h.fetchUpcomingAppointments(ctx, tokenData, patientID, office)
	if err != nil {
		log.Printf("WARNING: patient-resolve: failed to get appointments: %v", err)
		resp.AppointmentsStatus = appointmentsStatusError
		resp.Appointments = []PatientApptDetail{}
		resp.AppointmentsMessage = "Failed to retrieve appointments: " + err.Error()
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
	case appointmentsStatusSkipped:
		resp.Message = "Patient verified, appointment lookup skipped"
	case appointmentsStatusError:
		resp.Message = "Patient verified, appointment lookup unavailable"
	default:
		resp.Message = "Patient verified"
	}
}

// fetchUpcomingAppointments retrieves future appointments for a patient ID
// (current month + 5 months forward).
func (h *Handlers) fetchUpcomingAppointments(ctx context.Context, tokenData *domain.TokenData, patientID string, office *domain.OfficeConfig) ([]PatientApptDetail, error) {
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

		typeName := ""
		if len(a.AppointmentTypes) > 0 {
			if name, ok := office.AppointmentTypeName(a.AppointmentTypes[0]); ok {
				typeName = name
			}
		}

		detail := PatientApptDetail{
			ID:       a.ID,
			Date:     startTime.Format("Monday, January 2, 2006"),
			Time:     startTime.Format("3:04 PM"),
			Provider: office.FriendlyProviderName(a.Provider),
			Type:     typeName,
			Facility: friendlyFacilityName(a.Facility),
		}
		if err := h.addCancelToken(&detail, patientID, office, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("failed to create cancel token: %w", err)
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

// CancelAppointmentRequest is the expected JSON body for cancelling an appointment.
type CancelAppointmentRequest struct {
	AppointmentID int    `json:"appointmentId"`
	PatientID     string `json:"patientId,omitempty"`
	CancelToken   string `json:"cancelToken,omitempty"`
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
	if req.CancelToken == "" && !h.allowLegacyCancel {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "cancelToken is required. Please load appointments again and choose the appointment to cancel.",
		})
		return
	}
	var office *domain.OfficeConfig
	if req.Office != "" || req.CancelToken == "" {
		var err error
		office, err = resolveOffice(req.Office)
		if err != nil {
			json.NewEncoder(w).Encode(CancelAppointmentResponse{
				Status:  "error",
				Message: err.Error(),
			})
			return
		}
	}
	if req.CancelToken != "" {
		if req.PatientID == "" {
			json.NewEncoder(w).Encode(CancelAppointmentResponse{
				Status:  "error",
				Message: "patientId is required",
			})
			return
		}
		tokenOffice, err := h.applyCancelToken(&req, office, time.Now().UTC())
		if err != nil {
			json.NewEncoder(w).Encode(CancelAppointmentResponse{
				Status:  "error",
				Message: "Invalid or expired cancel token. Please load appointments again and choose the appointment to cancel.",
			})
			return
		}
		office = tokenOffice
	}

	log.Printf("cancel-appointment: request office=%s", office.ID)

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	// Cancel via AMD REST API
	if err := h.amdRestClient.CancelAppointment(r.Context(), tokenData, req.AppointmentID); err != nil {
		log.Printf("cancel-appointment: AMD error: %v", err)
		json.NewEncoder(w).Encode(CancelAppointmentResponse{
			Status:  "error",
			Message: "Failed to cancel appointment: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(CancelAppointmentResponse{
		Status:        "cancelled",
		AppointmentID: req.AppointmentID,
		Message:       "Appointment cancelled successfully",
	})
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

	bookingRequiresForce bool
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
	NoteID              string   `json:"noteId,omitempty"`
	NoteStatus          string   `json:"noteStatus,omitempty"`
	Message             string   `json:"message"`
	Missing             []string `json:"missing,omitempty"`
}

func buildBookAppointmentReceipt(req BookAppointmentRequest, office *domain.OfficeConfig, appointmentID int) BookAppointmentResponse {
	colIDStr := strconv.Itoa(req.ColumnID)
	providerName := ""
	if col, ok := office.Columns[colIDStr]; ok {
		providerName = col.DisplayName
	}
	if providerName == "" {
		providerName = office.ProviderDisplayName(strconv.Itoa(req.ProfileID))
	}
	appointmentTypeName, _ := office.AppointmentTypeName(req.AppointmentTypeID)

	return BookAppointmentResponse{
		Status:              "booked",
		AppointmentID:       appointmentID,
		PatientID:           req.PatientID,
		PatientName:         normalizeBookingPatientName(req.PatientName),
		ProviderName:        providerName,
		LocationName:        office.DisplayName,
		StartDatetime:       req.StartDatetime,
		Duration:            req.Duration,
		AppointmentTypeID:   req.AppointmentTypeID,
		AppointmentTypeName: appointmentTypeName,
		Message:             "Appointment booked successfully",
	}
}

func normalizeBookingPatientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	if parts := strings.SplitN(name, ",", 2); len(parts) == 2 {
		first := strings.TrimSpace(parts[1])
		last := strings.TrimSpace(parts[0])
		name = strings.TrimSpace(strings.Join([]string{first, last}, " "))
	}

	if name == strings.ToUpper(name) || name == strings.ToLower(name) {
		return cases.Title(language.English).String(strings.ToLower(name))
	}
	return name
}

func buildBookingPatientNote(appointmentID int, appointmentReason string, referringDoctor string) string {
	appointmentReason = normalizePatientNote(appointmentReason)
	referringDoctor = normalizePatientNote(referringDoctor)
	if appointmentReason == "" && referringDoctor == "" {
		return ""
	}
	if referringDoctor == "" {
		referringDoctor = "none"
	}

	lines := []string{
		fmt.Sprintf("Appointment ID: %d", appointmentID),
	}
	if appointmentReason != "" {
		lines = append(lines, "Appointment reason: "+appointmentReason)
	}
	if referringDoctor != "" {
		lines = append(lines, "Referring doctor: "+referringDoctor)
	}

	return strings.Join(lines, "\n")
}

// HandleBookAppointment books an appointment in AdvancedMD.
func (h *Handlers) HandleBookAppointment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req BookAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	var office *domain.OfficeConfig
	if req.Office != "" || req.BookingToken == "" {
		var err error
		office, err = resolveOffice(req.Office)
		if err != nil {
			json.NewEncoder(w).Encode(BookAppointmentResponse{
				Status:  "error",
				Message: err.Error(),
			})
			return
		}
	}
	if req.BookingToken != "" {
		tokenOffice, err := h.applyBookingToken(&req, office, time.Now().UTC())
		if err != nil {
			json.NewEncoder(w).Encode(BookAppointmentResponse{
				Status:  "error",
				Outcome: "invalid_booking_token",
				Message: "Invalid or expired booking token. Please check availability again and choose a slot.",
			})
			return
		}
		office = tokenOffice
	}

	log.Printf("book-appointment: request office=%s routing=%q bookingToken=%t legacyRaw=%t typeId=%d",
		office.ID, req.Routing, req.BookingToken != "", req.BookingToken == "", req.AppointmentTypeID)

	// Validate required fields
	if req.PatientID == "" {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "patientId is required"})
		return
	}
	if req.ColumnID == 0 {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "columnId is required"})
		return
	}
	if req.ProfileID == 0 {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "profileId is required"})
		return
	}
	if req.StartDatetime == "" {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "startDatetime is required"})
		return
	}
	if req.Duration == 0 {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "duration is required"})
		return
	}
	if err := validateOptionalDOB(req.DOB); err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: err.Error()})
		return
	}
	if notePreview := buildBookingPatientNote(0, req.AppointmentReason, req.ReferringDoctor); len([]rune(notePreview)) > maxPatientNoteLength {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("appointment note must be %d characters or fewer", maxPatientNoteLength),
		})
		return
	}

	// Validate columnId is allowed for this office
	colIDStr := strconv.Itoa(req.ColumnID)
	if !office.IsAllowedColumn(colIDStr) {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Column %d is not a valid provider column for %s", req.ColumnID, office.DisplayName),
		})
		return
	}
	column := office.Columns[colIDStr]
	if column.ProfileID != strconv.Itoa(req.ProfileID) {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Profile %d is not valid for column %d at %s", req.ProfileID, req.ColumnID, office.DisplayName),
		})
		return
	}

	// A Spring Hill routine-vision column is part of the office, but not part of
	// normal medical routing. Require the same routing lane used for availability.
	routingRule := domain.ParseRoutingRule(req.Routing)
	routingRule = effectiveRoutingForDOB(office, routingRule, req.DOB)
	if req.AppointmentTypeID == 0 {
		resolution := domain.ResolveAppointmentTypeForIntent(office, routingRule, domain.AppointmentIntent{
			VisitCategory: req.VisitCategory,
			VisitKind:     req.VisitKind,
			PatientStatus: req.PatientStatus,
			AgeBand:       req.AgeBand,
			DOB:           req.DOB,
			IsPostOp:      req.IsPostOp,
			VisitReason:   req.VisitReason,
		})
		if resolution.AppointmentTypeID == 0 {
			message := resolution.Message
			if message == "" {
				message = "Could not resolve appointment type from booking intent."
			}
			json.NewEncoder(w).Encode(BookAppointmentResponse{
				Status:  "error",
				Outcome: "appointment_type_unresolved",
				Message: message,
				Missing: resolution.Missing,
			})
			return
		}
		req.AppointmentTypeID = resolution.AppointmentTypeID
		log.Printf("book-appointment: resolved appointment type office=%s routing=%q typeId=%d typeName=%q",
			office.ID, routingRule, resolution.AppointmentTypeID, resolution.AppointmentTypeName)
	}
	routingColumns := office.ColumnsForRouting(routingRule)
	if routingColumns == nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Cannot book appointment with routing %q at %s", routingRule, office.DisplayName),
		})
		return
	}
	if !routingColumns[colIDStr] {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Column %d is not valid for routing %q at %s", req.ColumnID, routingRule, office.DisplayName),
		})
		return
	}
	// Resolve appointment type ID for current environment (prod IDs → env IDs)
	envTypeID, ok := domain.ResolveAppointmentTypeID(req.AppointmentTypeID)
	if !ok {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Invalid appointment type ID: %d. Valid types: 1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169", req.AppointmentTypeID),
		})
		return
	}

	// Resolve color from canonical (prod) type ID
	color, ok := office.AppointmentColor(req.AppointmentTypeID)
	if !ok {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Invalid appointment type ID: %d", req.AppointmentTypeID),
		})
		return
	}
	if !office.AllowsAppointmentType(req.AppointmentTypeID, routingRule) {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Appointment type %d is not valid for routing %q at %s", req.AppointmentTypeID, routingRule, office.DisplayName),
		})
		return
	}
	if !office.ColumnAllowsDOB(colIDStr, req.DOB) {
		message := fmt.Sprintf("%s requires patient age %d or older", column.ShortName, column.MinAgeYears)
		if req.DOB == "" {
			message = fmt.Sprintf("%s requires patient DOB to verify age %d or older", column.ShortName, column.MinAgeYears)
		}
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: message,
		})
		return
	}

	// Parse patient ID
	patientIDInt, err := strconv.Atoi(req.PatientID)
	if err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: "patientId must be numeric",
		})
		return
	}
	if req.BookingToken == "" && !h.allowRawSlotBooking {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Outcome: "booking_token_required",
			Message: "bookingToken is required. Please check availability again and choose one of the returned slots.",
		})
		return
	}

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	// Resolve facility ID from office config
	facilityIDInt, _ := strconv.Atoi(office.FacilityID)

	force := 0
	if req.bookingRequiresForce && isBachColumn(office, colIDStr) {
		force = 1
	}

	// Book via AMD REST API
	apptID, err := h.amdRestClient.BookAppointment(r.Context(), tokenData, clients.BookAppointmentParams{
		PatientID:     patientIDInt,
		ColumnID:      req.ColumnID,
		ProfileID:     req.ProfileID,
		StartDatetime: req.StartDatetime,
		Duration:      req.Duration,
		AppointmentType: []struct {
			ID int `json:"id"`
		}{{ID: envTypeID}},
		EpisodeID:  1,
		FacilityID: facilityIDInt,
		Color:      color,
		Force:      force,
	})
	if err != nil {
		log.Printf("book-appointment: AMD error: %v", err)

		// Handle 409 conflict errors with clear messages
		errStr := err.Error()
		if strings.Contains(errStr, "conflict") {
			json.NewEncoder(w).Encode(BookAppointmentResponse{
				Status:  "error",
				Outcome: "slot_unavailable",
				Message: "This time slot is no longer available. Please check availability again and choose a different slot.",
			})
			return
		}

		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: "Failed to book appointment: " + err.Error(),
		})
		return
	}

	log.Printf("book-appointment: success office=%s", office.ID)

	receipt := buildBookAppointmentReceipt(req, office, apptID)
	if note := buildBookingPatientNote(apptID, req.AppointmentReason, req.ReferringDoctor); note != "" {
		if h.amdClient == nil {
			receipt.Status = "partial"
			receipt.NoteStatus = "failed"
			receipt.Message = "Appointment booked but note failed: AdvancedMD note client is not configured"
			json.NewEncoder(w).Encode(receipt)
			return
		}

		noteID, err := h.amdClient.SavePatientNote(r.Context(), tokenData, clients.SavePatientNoteParams{
			PatientID:   req.PatientID,
			ProfileID:   strconv.Itoa(req.ProfileID),
			NoteTypeFID: clients.DefaultPatientNoteTypeFID,
			Note:        note,
		})
		if err != nil {
			receipt.Status = "partial"
			receipt.NoteStatus = "failed"
			receipt.Message = "Appointment booked but note failed: " + err.Error()
			json.NewEncoder(w).Encode(receipt)
			return
		}
		receipt.NoteID = noteID
		receipt.NoteStatus = "saved"
		receipt.Message = "Appointment booked and note saved successfully"
	}

	json.NewEncoder(w).Encode(receipt)
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

const availabilitySearchForwardDays = 14

func availabilityDateShifted(requestedDate, searchStartDate, actualDate string) bool {
	if actualDate != "" {
		return actualDate != requestedDate
	}
	return searchStartDate != requestedDate
}

func noAvailabilityMessage(searchStartDate, searchEndDate string) string {
	return fmt.Sprintf("No availability was found from %s through %s. Do not search this same window again unless the patient changes date, provider, office, or appointment type.", searchStartDate, searchEndDate)
}

func incompleteAvailabilityMessage(searchStartDate, searchEndDate string, unavailableDataChecks int) string {
	return fmt.Sprintf("Availability could not be fully checked from %s through %s because appointment data was unavailable for %d provider-date checks. Retry once; if it still cannot be checked, ask for different preferences.", searchStartDate, searchEndDate, unavailableDataChecks)
}

func flattenAvailabilitySlots(providers []domain.ProviderAvailability) []domain.AvailabilitySlotOption {
	var slots []domain.AvailabilitySlotOption
	for _, provider := range providers {
		if provider.TotalAvailable == 0 {
			continue
		}
		for _, slot := range provider.Slots {
			slots = append(slots, domain.AvailabilitySlotOption{
				Provider:          provider.Name,
				Time:              slot.Time,
				DateTime:          slot.DateTime,
				ColumnID:          provider.ColumnID,
				ProfileID:         provider.ProfileID,
				Duration:          provider.SlotDuration,
				SameStartBooked:   slot.SameStartBooked,
				SameStartCapacity: slot.SameStartCapacity,
				RequiresForce:     slot.RequiresForce,
			})
		}
	}
	return slots
}

func filterColumnsForRouting(columns []domain.SchedulerColumn, office *domain.OfficeConfig, routing domain.RoutingRule) []domain.SchedulerColumn {
	routingColumns := office.ColumnsForRouting(routing)
	if routingColumns == nil {
		return nil
	}

	filtered := make([]domain.SchedulerColumn, 0, len(columns))
	for _, col := range columns {
		if routingColumns[col.ID] {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func filterColumnsForDOB(columns []domain.SchedulerColumn, office *domain.OfficeConfig, dob string) []domain.SchedulerColumn {
	filtered := make([]domain.SchedulerColumn, 0, len(columns))
	for _, col := range columns {
		if office.ColumnAllowsDOB(col.ID, dob) {
			filtered = append(filtered, col)
		}
	}
	return filtered
}

func officeSupportsRouting(office *domain.OfficeConfig, routing domain.RoutingRule) bool {
	return len(office.ColumnsForRouting(routing)) > 0
}

func isBachColumn(office *domain.OfficeConfig, columnID string) bool {
	if office == nil {
		return false
	}
	col, ok := office.Columns[columnID]
	return ok && col.MatchKey == "BACH"
}

func sameStartCapacityForColumn(office *domain.OfficeConfig, col domain.SchedulerColumn) int {
	if isBachColumn(office, col.ID) {
		return bachSameStartCapacity
	}
	return 1
}

func (h *Handlers) getSchedulerSetup(ctx context.Context, tokenData *domain.TokenData, now time.Time) (*domain.SchedulerSetup, error) {
	h.schedulerSetupMu.Lock()
	defer h.schedulerSetupMu.Unlock()

	if h.schedulerSetup != nil && now.Before(h.schedulerSetupExpiresAt) {
		return h.schedulerSetup, nil
	}

	var (
		setup *domain.SchedulerSetup
		err   error
	)
	if h.amdClient == nil {
		err = fmt.Errorf("scheduler setup client is not configured")
	} else {
		setup, err = h.amdClient.GetSchedulerSetup(ctx, tokenData)
	}
	if err != nil {
		if h.schedulerSetup != nil {
			log.Printf("WARNING: scheduler setup refresh failed; using cached setup: %v", err)
			h.schedulerSetupExpiresAt = now.Add(time.Minute)
			return h.schedulerSetup, nil
		}
		return nil, err
	}

	h.schedulerSetup = setup
	h.schedulerSetupExpiresAt = now.Add(schedulerSetupCacheTTL)
	return setup, nil
}

// HandleGetAvailability returns available appointment slots for providers.
func (h *Handlers) HandleGetAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse request body
	var req AvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	// Validate required date field
	if req.Date == "" {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "date is required (YYYY-MM-DD format)"})
		return
	}
	originalRequestedDate := req.Date

	// Parse start date
	startDate, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Invalid date format. Use YYYY-MM-DD."})
		return
	}
	if err := validateOptionalDOB(req.DOB); err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: err.Error()})
		return
	}

	// Reject same-day or past date searches
	todayEastern := time.Now().In(eastern)
	todayStr := todayEastern.Format("2006-01-02")
	if startDate.Format("2006-01-02") <= todayStr {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Same-day and past-date appointments are not available. Please search for tomorrow or later."})
		return
	}

	// Preauth: auto-advance to 14 days out if requested date is too soon
	if req.PreauthRequired {
		startDate, req.Date = enforcePreauthMinDate(startDate, time.Now().In(eastern))
	}
	searchStartDate := startDate.Format("2006-01-02")
	maxDate := startDate.AddDate(0, 0, availabilitySearchForwardDays)
	searchEndDate := maxDate.Format("2006-01-02")

	// Resolve office config
	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	effectiveRouting := domain.ParseRoutingRule(req.Routing)
	effectiveRouting = effectiveRoutingForDOB(office, effectiveRouting, req.DOB)

	log.Printf("availability: date=%s provider=%q office=%s routing=%q effectiveRouting=%q preauthRequired=%v", req.Date, req.Provider, office.ID, req.Routing, effectiveRouting, req.PreauthRequired)

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	// Get scheduler setup from cache when fresh. Appointments and block
	// holds below are still fetched live for availability freshness.
	setup, err := h.getSchedulerSetup(r.Context(), tokenData, time.Now().UTC())
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Failed to get scheduler setup: " + err.Error(),
		})
		return
	}

	// Build lookup maps
	profileMap := make(map[string]domain.SchedulerProfile)
	for _, p := range setup.Profiles {
		profileMap[p.ID] = p
	}

	facilityMap := make(map[string]domain.SchedulerFacility)
	for _, f := range setup.Facilities {
		facilityMap[f.ID] = f
	}

	// Filter columns to office's allowed providers
	var allowedColumns []domain.SchedulerColumn
	for _, col := range setup.Columns {
		if !office.IsAllowedColumn(col.ID) {
			continue
		}
		if col.FacilityID != office.FacilityID {
			continue
		}
		if req.Provider != "" {
			profile, ok := profileMap[col.ProfileID]
			if !ok {
				continue
			}
			normalizedProvider := strings.ToUpper(domain.NormalizeForLookup(req.Provider))
			if !strings.Contains(strings.ToUpper(domain.NormalizeForLookup(profile.Name)), normalizedProvider) &&
				!strings.Contains(strings.ToUpper(domain.NormalizeForLookup(col.Name)), normalizedProvider) {
				continue
			}
		}
		allowedColumns = append(allowedColumns, col)
	}

	// Apply routing filter. Empty/unknown routing defaults to RoutingAll,
	// which deliberately excludes Spring Hill's routine-vision column.
	allowedColumns = filterColumnsForRouting(allowedColumns, office, effectiveRouting)
	allowedColumns = filterColumnsForDOB(allowedColumns, office, req.DOB)

	if len(allowedColumns) == 0 {
		if req.Provider != "" {
			json.NewEncoder(w).Encode(ErrorResponse{
				Status: "error",
				Message: fmt.Sprintf("No provider found matching %q. Valid providers: %s",
					req.Provider, strings.Join(office.ValidProviderNames(), ", ")),
			})
			return
		}
		json.NewEncoder(w).Encode(domain.AvailabilityResponse{
			Status:                domain.AvailabilityStatusSuccess,
			Outcome:               domain.AvailabilityOutcomeNoEligibleProviders,
			AvailabilityFound:     false,
			RequestedDate:         originalRequestedDate,
			ShouldRetrySameSearch: false,
			NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
			Message:               "No eligible providers found for this office, routing, provider, and DOB.",
			Slots:                 []domain.AvailabilitySlotOption{},
		})
		return
	}

	nowEastern := time.Now().In(eastern)

	// Try the requested date first, then auto-search forward up to 14 days
	searchDate := startDate
	var providers []domain.ProviderAvailability
	searchIncomplete := false
	unavailableDataChecks := 0

	for !searchDate.After(maxDate) {
		dateStr := searchDate.Format("2006-01-02")

		// Only fetch columns that work this weekday — skip non-working providers
		var workingColumnIDs []string
		workingColumnSet := make(map[string]bool)
		for _, col := range allowedColumns {
			if col.WorksOnDay(searchDate.Weekday()) {
				workingColumnIDs = append(workingColumnIDs, col.ID)
				workingColumnSet[col.ID] = true
			}
		}
		if len(workingColumnIDs) == 0 {
			searchDate = searchDate.AddDate(0, 0, 1)
			log.Printf("availability: no providers work on %s, skipping", dateStr)
			continue
		}

		// Fetch appointments and block holds concurrently (independent data)
		var appointmentsByColumn map[string][]domain.Appointment
		var blockHoldsByColumn map[string][]domain.BlockHold
		var fetchWg sync.WaitGroup
		fetchWg.Add(2)
		go func() {
			defer fetchWg.Done()
			appointmentsByColumn = h.amdRestClient.GetAppointmentsForColumns(r.Context(), tokenData, workingColumnIDs, dateStr)
		}()
		go func() {
			defer fetchWg.Done()
			blockHoldsByColumn = h.amdRestClient.GetBlockHoldsForColumns(r.Context(), tokenData, workingColumnIDs, dateStr)
		}()
		fetchWg.Wait()

		// Calculate availability for each provider
		providers = nil
		for _, col := range allowedColumns {
			if !workingColumnSet[col.ID] {
				continue
			}
			// Skip columns where appointment data couldn't be fetched —
			// safer to omit than to show all slots as available
			if _, ok := appointmentsByColumn[col.ID]; !ok {
				searchIncomplete = true
				unavailableDataChecks++
				log.Printf("availability: skipping column %s — appointment data unavailable", col.ID)
				continue
			}
			profile := profileMap[col.ProfileID]
			facility := facilityMap[col.FacilityID]

			displayName := ""
			if officeCol, ok := office.Columns[col.ID]; ok {
				displayName = officeCol.DisplayName
			}
			if displayName == "" {
				displayName = office.ProviderDisplayName(col.ProfileID)
			}
			if displayName == "" {
				displayName = profile.Name
			}

			allSlots := calculateAvailableSlots(office, col, appointmentsByColumn[col.ID], blockHoldsByColumn[col.ID], searchDate, nowEastern)

			colID, _ := strconv.Atoi(col.ID)
			profID, _ := strconv.Atoi(col.ProfileID)

			pa := domain.ProviderAvailability{
				Name:           displayName,
				ColumnID:       colID,
				ProfileID:      profID,
				Facility:       facility.Name,
				SlotDuration:   col.Interval,
				TotalAvailable: len(allSlots),
			}

			if len(allSlots) > 0 {
				pa.FirstAvailable = allSlots[0].Time
				pa.LastAvailable = allSlots[len(allSlots)-1].Time
				if len(allSlots) > 5 {
					pa.Slots = allSlots[:5]
				} else {
					pa.Slots = allSlots
				}
			} else {
				pa.Slots = []domain.AvailableSlot{}
			}

			providers = append(providers, pa)
		}

		// Check if any provider has availability
		hasAvailability := false
		for _, p := range providers {
			if p.TotalAvailable > 0 {
				hasAvailability = true
				break
			}
		}

		if hasAvailability {
			break
		}

		// No availability — try the next day
		searchDate = searchDate.AddDate(0, 0, 1)
		log.Printf("availability: no slots on %s, searching forward to %s", dateStr, searchDate.Format("2006-01-02"))
	}

	// Check if any provider has availability after the search loop
	hasAnyAvailability := false
	for _, p := range providers {
		if p.TotalAvailable > 0 {
			hasAnyAvailability = true
			break
		}
	}

	if !hasAnyAvailability {
		if searchIncomplete {
			json.NewEncoder(w).Encode(domain.AvailabilityResponse{
				Status:                domain.AvailabilityStatusError,
				Outcome:               domain.AvailabilityOutcomeSearchIncomplete,
				AvailabilityFound:     false,
				RequestedDate:         originalRequestedDate,
				ShouldRetrySameSearch: true,
				NextAction:            domain.AvailabilityNextActionRetryOnceThenAskPreferences,
				SearchedFrom:          searchStartDate,
				SearchedThrough:       searchEndDate,
				Message:               incompleteAvailabilityMessage(searchStartDate, searchEndDate, unavailableDataChecks),
				Slots:                 []domain.AvailabilitySlotOption{},
			})
			return
		}

		json.NewEncoder(w).Encode(domain.AvailabilityResponse{
			Status:                domain.AvailabilityStatusSuccess,
			Outcome:               domain.AvailabilityOutcomeNoAvailability,
			AvailabilityFound:     false,
			RequestedDate:         originalRequestedDate,
			ShouldRetrySameSearch: false,
			NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
			SearchedFrom:          searchStartDate,
			SearchedThrough:       searchEndDate,
			Message:               noAvailabilityMessage(searchStartDate, searchEndDate),
			Slots:                 []domain.AvailabilitySlotOption{},
		})
		return
	}

	actualDate := searchDate.Format("2006-01-02")
	slots, err := h.addBookingTokens(flattenAvailabilitySlots(providers), office, effectiveRouting, time.Now().UTC())
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Failed to create booking tokens: " + err.Error(),
		})
		return
	}
	json.NewEncoder(w).Encode(domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusSuccess,
		Outcome:               domain.AvailabilityOutcomeFound,
		AvailabilityFound:     true,
		RequestedDate:         originalRequestedDate,
		ShouldRetrySameSearch: false,
		NextAction:            domain.AvailabilityNextActionOfferSlots,
		ActualDate:            actualDate,
		DateShifted:           availabilityDateShifted(originalRequestedDate, searchStartDate, actualDate),
		Slots:                 slots,
	})
}

// calculateAvailableSlots generates available time slots for a column on a single day.
// nowEastern is used to filter out past slots when the date is today.
func calculateAvailableSlots(office *domain.OfficeConfig, col domain.SchedulerColumn, appointments []domain.Appointment, blockHolds []domain.BlockHold, date time.Time, nowEastern time.Time) []domain.AvailableSlot {
	var slots []domain.AvailableSlot

	// Skip if provider doesn't work this day
	if !col.WorksOnDay(date.Weekday()) {
		return slots
	}

	// Get work hours
	workStart, workEnd, err := col.ParseWorkHours(date)
	if err != nil {
		return slots
	}

	// Determine cutoff for past slots: if date is today, skip slots before now + 30 min
	today := nowEastern.Format("2006-01-02")
	isToday := date.Format("2006-01-02") == today
	cutoff := nowEastern.Add(30 * time.Minute)

	interval := time.Duration(col.Interval) * time.Minute
	if interval == 0 {
		interval = 15 * time.Minute
	}

	sameStartCapacity := sameStartCapacityForColumn(office, col)

	for slotTime := workStart; slotTime.Before(workEnd); slotTime = slotTime.Add(interval) {
		// Filter past slots
		if isToday {
			slotInEastern := time.Date(slotTime.Year(), slotTime.Month(), slotTime.Day(),
				slotTime.Hour(), slotTime.Minute(), 0, 0, nowEastern.Location())
			if slotInEastern.Before(cutoff) {
				continue
			}
		}

		if domain.IsBlockedByHold(slotTime, interval, blockHolds) {
			continue
		}

		// AMD 4101: Block if any different-start appointment overlaps this slot's full booking range.
		if hasDifferentStartOverlappingAppointment(slotTime, interval, appointments) {
			continue
		}

		// AMD 4186: Check same-start-time appointment count against per-column capacity.
		sameStartCount := countSameStartAppointments(slotTime, appointments)
		if sameStartCount >= sameStartCapacity {
			continue
		}

		slot := domain.AvailableSlot{
			Time:     domain.FormatSlotTime(slotTime),
			DateTime: domain.FormatSlotDateTime(slotTime),
		}
		if sameStartCount > 0 {
			slot.SameStartBooked = sameStartCount
			slot.SameStartCapacity = sameStartCapacity
			slot.RequiresForce = isBachColumn(office, col.ID)
		}
		slots = append(slots, slot)
	}

	return slots
}

// hasDifferentStartOverlappingAppointment checks if a different-start appointment
// overlaps the full booking range [slotTime, slotTime+slotDuration). Same-start
// appointments are handled separately as per-column capacity because AMD's 4186
// rule is distinct from 4101 duration-overlap blocking.
func hasDifferentStartOverlappingAppointment(slotTime time.Time, slotDuration time.Duration, appointments []domain.Appointment) bool {
	slotEnd := slotTime.Add(slotDuration)
	for _, appt := range appointments {
		if appt.StartDateTime.Equal(slotTime) {
			continue
		}
		apptEnd := appt.StartDateTime.Add(time.Duration(appt.Duration) * time.Minute)
		// Two intervals overlap when each starts before the other ends
		if slotTime.Before(apptEnd) && appt.StartDateTime.Before(slotEnd) {
			return true
		}
	}
	return false
}

// countSameStartAppointments counts appointments that start at exactly the given slot time.
// AMD returns error 4186 when this count exceeds maxApptsPerSlot.
func countSameStartAppointments(slotTime time.Time, appointments []domain.Appointment) int {
	count := 0
	for _, appt := range appointments {
		if appt.StartDateTime.Equal(slotTime) {
			count++
		}
	}
	return count
}

// enforcePreauthMinDate advances the requested date to 14 days from now if it's too soon.
// Returns the (possibly advanced) date and its YYYY-MM-DD string.
func enforcePreauthMinDate(requestedDate time.Time, now time.Time) (time.Time, string) {
	// Truncate to date-only (midnight) so time-of-day doesn't affect the comparison
	minDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 14)
	if requestedDate.Before(minDate) {
		log.Printf("availability: preauth required — auto-advanced to %s (14-day minimum)", minDate.Format("2006-01-02"))
		return minDate, minDate.Format("2006-01-02")
	}
	return requestedDate, requestedDate.Format("2006-01-02")
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
	insuranceMode := domain.InsuranceModeForCoverage(req.CoverageType)
	if insuranceMode == domain.InsuranceModeVision && !officeSupportsRouting(office, domain.RoutingOpticalOnly) {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: fmt.Sprintf("Routine vision coverage is not supported at %s. Route the patient to Spring Hill routine vision scheduling.", office.DisplayName),
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
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	// End-date old plan if insplan ID provided
	if req.InsPlanID != "" {
		if err := h.amdClient.EndDateInsurance(r.Context(), tokenData, req.PatientID, req.InsPlanID); err != nil {
			json.NewEncoder(w).Encode(UpdateInsuranceResponse{
				Status:  "error",
				Message: "Failed to end-date existing insurance: " + err.Error(),
			})
			return
		}
	}

	// Add new insurance plan
	if err := h.amdClient.AddInsurance(r.Context(), tokenData, req.PatientID, req.RespPartyID, insEntry.CarrierID, req.SubscriberNum); err != nil {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "Failed to add new insurance: " + err.Error(),
		})
		return
	}

	routing := insEntry.Routing
	routing = effectiveRoutingForDOB(office, routing, req.DOB)
	_, ambiguous := domain.RoutingForDemographicInsurance(insEntry.CarrierID, req.Insurance, office)

	json.NewEncoder(w).Encode(UpdateInsuranceResponse{
		Status:           "updated",
		PatientID:        req.PatientID,
		OldInsurance:     req.OldInsurance,
		NewInsurance:     req.Insurance,
		Routing:          string(routing),
		AllowedProviders: office.ProvidersForRoutingAndDOB(routing, req.DOB),
		RoutingAmbiguous: ambiguous,
		PreauthRequired:  insEntry.PreauthRequired,
		Message:          "Insurance updated successfully",
	})
}

// AddPatientNoteRequest is the expected JSON body for adding a patient note.
type AddPatientNoteRequest struct {
	PatientID string `json:"patientId"`
	Note      string `json:"note"`
	Office    string `json:"office,omitempty"`
}

// AddPatientNoteResponse is returned after saving a patient note.
type AddPatientNoteResponse struct {
	Status    string `json:"status"`
	PatientID string `json:"patientId,omitempty"`
	NoteID    string `json:"noteId,omitempty"`
	Message   string `json:"message,omitempty"`
}

// HandleAddPatientNote saves an appointment note on an existing patient.
func (h *Handlers) HandleAddPatientNote(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req AddPatientNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	patientID := domain.StripPatientPrefix(strings.TrimSpace(req.PatientID))
	note := normalizePatientNote(req.Note)
	if patientID == "" {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: "patientId is required",
		})
		return
	}
	if _, err := strconv.Atoi(patientID); err != nil {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: "patientId must be numeric",
		})
		return
	}
	if note == "" {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: "note is required",
		})
		return
	}
	if len([]rune(note)) > maxPatientNoteLength {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: fmt.Sprintf("note must be %d characters or fewer", maxPatientNoteLength),
		})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	noteID, err := h.amdClient.SavePatientNote(r.Context(), tokenData, clients.SavePatientNoteParams{
		PatientID:   patientID,
		ProfileID:   office.DefaultProfileID,
		NoteTypeFID: clients.DefaultPatientNoteTypeFID,
		Note:        note,
	})
	if err != nil {
		json.NewEncoder(w).Encode(AddPatientNoteResponse{
			Status:    "error",
			PatientID: patientID,
			Message:   "Failed to save patient note: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(AddPatientNoteResponse{
		Status:    "saved",
		PatientID: patientID,
		NoteID:    noteID,
		Message:   "Patient note saved",
	})
}

func normalizePatientNote(note string) string {
	return strings.TrimSpace(note)
}
