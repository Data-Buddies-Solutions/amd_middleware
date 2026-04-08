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

// ElevenLabsWebhookResponse is the response format for ElevenLabs conversation initiation webhook.
type ElevenLabsWebhookResponse struct {
	Type             string            `json:"type"`
	DynamicVariables map[string]interface{} `json:"dynamic_variables"`
}

// VerifyPatientRequest is the expected JSON body for patient verification.
type VerifyPatientRequest struct {
	LastName  string `json:"lastName"`
	DOB       string `json:"dob"`
	FirstName string `json:"firstName,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Office    string `json:"office,omitempty"`
}

// VerifyPatientResponse is returned on successful patient verification.
type VerifyPatientResponse struct {
	Status             string         `json:"status"`
	PatientID          string         `json:"patientId,omitempty"`
	Name               string         `json:"name,omitempty"`
	DOB                string         `json:"dob,omitempty"`
	Phone              string         `json:"phone,omitempty"`
	InsuranceCarrier   string         `json:"insuranceCarrier,omitempty"`
	InsuranceCarrierID string         `json:"insuranceCarrierId,omitempty"`
	InsPlanID          string         `json:"insPlanId,omitempty"`
	RespPartyID        string         `json:"respPartyId,omitempty"`
	Routing            string         `json:"routing,omitempty"`
	AllowedProviders   []string       `json:"allowedProviders,omitempty"`
	RoutingAmbiguous   bool           `json:"routingAmbiguous,omitempty"`
	Message            string         `json:"message,omitempty"`
	Matches            []PatientMatch `json:"matches,omitempty"`
}

// PatientMatch provides minimal info for disambiguation.
type PatientMatch struct {
	FirstName string `json:"firstName"`
}

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	tokenManager  *auth.TokenManager
	amdClient     *clients.AdvancedMDClient
	amdRestClient *clients.AdvancedMDRestClient
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(tm *auth.TokenManager, amdClient *clients.AdvancedMDClient, amdRestClient *clients.AdvancedMDRestClient) *Handlers {
	return &Handlers{
		tokenManager:  tm,
		amdClient:     amdClient,
		amdRestClient: amdRestClient,
	}
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

// HandleHealth returns a simple health check response.
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// TokenRequest is the optional JSON body for the token/precall webhook.
type TokenRequest struct {
	Office string `json:"office,omitempty"`
}

// HandleGetToken returns the cached AdvancedMD token for ElevenLabs agents.
// Accepts POST only (for ElevenLabs conversation initiation webhook).
func (h *Handlers) HandleGetToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	log.Printf("token: received webhook request")

	// Parse optional office from request body
	var req TokenRequest
	json.NewDecoder(r.Body).Decode(&req) // ignore errors — body may be empty

	officePhone := req.Office
	if officePhone == "" {
		officePhone = domain.DefaultPhone
	}

	office, err := resolveOffice(officePhone)
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}
	_ = office // resolved for validation only; phone key is returned below

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Failed to get token: " + err.Error(),
		})
		return
	}

	nowEST := time.Now().In(eastern)

	dynamicVars := map[string]interface{}{
		"amd_token":         tokenData.Token,
		"amd_rest_api_base": tokenData.RestApiBase,
		"patient_id":        "1",
		"current_date":      nowEST.Format("Monday, January 2, 2006"),
		"current_time":      nowEST.Format("3:04 PM"),
		"office":            officePhone,
	}

	json.NewEncoder(w).Encode(ElevenLabsWebhookResponse{
		Type:             "conversation_initiation_client_data",
		DynamicVariables: dynamicVars,
	})
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

	log.Printf("add-patient: received request: firstName=%q lastName=%q dob=%q phone=%q email=%q street=%q aptSuite=%q city=%q state=%q zip=%q sex=%q insurance=%q subscriberName=%q subscriberNum=%q office=%q",
		req.FirstName, req.LastName, req.DOB, req.Phone, req.Email, req.Street, req.AptSuite, req.City, req.State, req.Zip, req.Sex, req.Insurance, req.SubscriberName, req.SubscriberNum, office.ID)

	// Validate required fields (aptSuite is optional)
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
	if req.Email == "" {
		missing = append(missing, "email")
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
	if len(missing) > 0 {
		json.NewEncoder(w).Encode(AddPatientResponse{
			Status:  "error",
			Message: fmt.Sprintf("Missing required fields: %s", strings.Join(missing, ", ")),
		})
		return
	}

	// Normalize inputs
	normalizedDOB := domain.NormalizeDOB(req.DOB)
	formattedPhone := domain.FormatPhone(req.Phone)
	normalizedSex := domain.NormalizeSex(req.Sex)
	normalizedFirstName := domain.StripDiacritics(req.FirstName)
	normalizedLastName := domain.StripDiacritics(req.LastName)

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
		Email:     req.Email,
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
	insEntry, ok := domain.LookupInsurance(req.Insurance)
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
	if domain.IsMinor(normalizedDOB) && routing != domain.RoutingNotAccepted {
		routing = office.PediatricRouting
	}

	json.NewEncoder(w).Encode(AddPatientResponse{
		Status:           "created",
		PatientID:        strippedID,
		Name:             patientName,
		DOB:              normalizedDOB,
		Routing:          string(routing),
		AllowedProviders: office.ProvidersForRouting(routing),
		PreauthRequired:  insEntry.PreauthRequired,
		Message:          "Patient created and insurance attached successfully",
	})
}

// HandleVerifyPatient looks up a patient by name and DOB.
func (h *Handlers) HandleVerifyPatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse request body
	var req VerifyPatientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(VerifyPatientResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(VerifyPatientResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	// Validate required fields:
	//   phone + firstName  — search by phone, filter by first name
	//   phone + dob        — search by phone, filter by DOB
	//   lastName + dob     — search by name, filter by DOB
	hasPhone := req.Phone != ""
	hasLastName := req.LastName != ""
	hasFirstName := req.FirstName != ""
	hasDOB := req.DOB != ""

	if hasPhone && (hasFirstName || hasDOB) {
		// valid: phone + firstName, phone + dob, or phone + both
	} else if hasLastName && hasDOB {
		// valid: lastName + dob
	} else {
		json.NewEncoder(w).Encode(VerifyPatientResponse{
			Status:  "error",
			Message: "Provide phone + firstName, phone + dob, or lastName + dob",
		})
		return
	}

	// Normalize inputs
	var normalizedDOB string
	if hasDOB {
		normalizedDOB = domain.NormalizeDOB(req.DOB)
	}

	// Get token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(VerifyPatientResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	// Call AdvancedMD lookuppatient API — by phone or by name
	var patients []domain.Patient
	if hasPhone {
		digits := domain.NormalizePhoneDigits(req.Phone)
		patients, err = h.amdClient.LookupPatientByPhone(r.Context(), tokenData, digits)
		if err != nil {
			json.NewEncoder(w).Encode(VerifyPatientResponse{
				Status:  "error",
				Message: "Failed to lookup patient by phone: " + err.Error(),
			})
			return
		}
		log.Printf("verify-patient: lookup phone=%q returned %d patients", digits, len(patients))
	} else {
		normalizedLastName := domain.StripDiacritics(req.LastName)
		normalizedFirstName := domain.StripDiacritics(req.FirstName)
		patients, err = h.amdClient.LookupPatient(r.Context(), tokenData, normalizedLastName, normalizedFirstName)
		if err != nil {
			json.NewEncoder(w).Encode(VerifyPatientResponse{
				Status:  "error",
				Message: "Failed to lookup patient: " + err.Error(),
			})
			return
		}
		log.Printf("verify-patient: lookup lastName=%q returned %d patients", normalizedLastName, len(patients))
	}
	for i, p := range patients {
		log.Printf("verify-patient: result[%d] id=%s name=%q dob=%q", i, p.ID, p.FullName, p.DOB)
	}

	// Filter patients — by DOB if provided, otherwise by first name (phone + firstName path)
	var matchingPatients []domain.Patient
	if hasDOB {
		for _, p := range patients {
			if domain.NormalizeDOB(p.DOB) == normalizedDOB {
				matchingPatients = append(matchingPatients, p)
			}
		}
	} else {
		upperFirstName := strings.ToUpper(domain.StripDiacritics(req.FirstName))
		for _, p := range patients {
			if strings.HasPrefix(p.FirstName, upperFirstName) {
				matchingPatients = append(matchingPatients, p)
			}
		}
	}

	// Handle results
	switch len(matchingPatients) {
	case 0:
		json.NewEncoder(w).Encode(VerifyPatientResponse{
			Status:  "not_found",
			Message: "No patient found matching the provided information",
		})
		return

	case 1:
		p := matchingPatients[0]
		demoResult, err := h.amdClient.GetDemographic(r.Context(), tokenData, p.ID)
		if err != nil {
			log.Printf("WARNING: failed to get demographics for patient %s: %v", p.ID, err)
		}

		resp := VerifyPatientResponse{
			Status:    "verified",
			PatientID: p.ID,
			Name:      p.FullName,
			DOB:       p.DOB,
			Phone:     p.Phone,
		}

		if demoResult != nil {
			resp.InsuranceCarrier = demoResult.CarrierName
			resp.InsPlanID = demoResult.InsPlanID
			resp.RespPartyID = demoResult.RespPartyID

			if demoResult.CarrierID != "" {
				resp.InsuranceCarrierID = demoResult.CarrierID
				routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
				resp.Routing = string(routing)
				resp.AllowedProviders = office.ProvidersForRouting(routing)
				resp.RoutingAmbiguous = ambiguous
			}
		}

		// Pediatric override: under-18 patients → office pediatric routing
		if domain.IsMinor(p.DOB) && resp.Routing != "" && resp.Routing != string(domain.RoutingNotAccepted) {
			resp.Routing = string(office.PediatricRouting)
			resp.AllowedProviders = office.ProvidersForRouting(office.PediatricRouting)
			resp.RoutingAmbiguous = false
		}

		json.NewEncoder(w).Encode(resp)
		return

	default:
		if !hasDOB {
			// Phone + firstName path: first name already used as filter, ask for DOB to disambiguate
			json.NewEncoder(w).Encode(VerifyPatientResponse{
				Status:  "multiple_matches",
				Message: fmt.Sprintf("Found %d patients with that name and phone number. Please provide date of birth.", len(matchingPatients)),
			})
			return
		}

		// DOB path: try to disambiguate by first name
		if hasFirstName {
			upperFirstName := strings.ToUpper(domain.StripDiacritics(req.FirstName))
			for _, p := range matchingPatients {
				if strings.HasPrefix(p.FirstName, upperFirstName) {
					demoResult, err := h.amdClient.GetDemographic(r.Context(), tokenData, p.ID)
					if err != nil {
						log.Printf("WARNING: failed to get demographics for patient %s: %v", p.ID, err)
					}

					resp := VerifyPatientResponse{
						Status:    "verified",
						PatientID: p.ID,
						Name:      p.FullName,
						DOB:       p.DOB,
						Phone:     p.Phone,
					}

					if demoResult != nil {
						resp.InsuranceCarrier = demoResult.CarrierName
						resp.InsPlanID = demoResult.InsPlanID
						resp.RespPartyID = demoResult.RespPartyID

						if demoResult.CarrierID != "" {
							resp.InsuranceCarrierID = demoResult.CarrierID
							routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
							resp.Routing = string(routing)
							resp.AllowedProviders = office.ProvidersForRouting(routing)
							resp.RoutingAmbiguous = ambiguous
						}
					}

					// Pediatric override: under-18 patients → office pediatric routing
					if domain.IsMinor(p.DOB) && resp.Routing != "" && resp.Routing != string(domain.RoutingNotAccepted) {
						resp.Routing = string(office.PediatricRouting)
						resp.AllowedProviders = office.ProvidersForRouting(office.PediatricRouting)
						resp.RoutingAmbiguous = false
					}

					json.NewEncoder(w).Encode(resp)
					return
				}
			}
			json.NewEncoder(w).Encode(VerifyPatientResponse{
				Status:  "not_found",
				Message: "No patient found matching that first name",
			})
			return
		}

		// Return list of first names for disambiguation
		var matches []PatientMatch
		for _, p := range matchingPatients {
			matches = append(matches, PatientMatch{FirstName: p.FirstName})
		}
		json.NewEncoder(w).Encode(VerifyPatientResponse{
			Status:  "multiple_matches",
			Message: fmt.Sprintf("Found %d patients with that last name and DOB. Please provide first name.", len(matchingPatients)),
			Matches: matches,
		})
	}
}

// GetAppointmentsRequest is the expected JSON body for patient appointment lookup.
type GetAppointmentsRequest struct {
	PatientID string `json:"patientId"`
	Office    string `json:"office,omitempty"`
}

// PatientApptResponse is returned on successful appointment lookup.
type PatientApptResponse struct {
	Status       string              `json:"status"`
	PatientID    string              `json:"patientId,omitempty"`
	Appointments []PatientApptDetail `json:"appointments,omitempty"`
	Message      string              `json:"message,omitempty"`
}

// PatientApptDetail is a single appointment formatted for LLM consumption.
type PatientApptDetail struct {
	ID        int    `json:"id"`                  // AMD appointment ID — for cancel_appt
	Date      string `json:"date"`                // Human-readable (e.g., "Wednesday, March 18, 2026")
	Time      string `json:"time"`                // e.g., "12:00 PM"
	Provider  string `json:"provider,omitempty"`   // e.g., "Dr. Austin Bach"
	Type      string `json:"type,omitempty"`       // e.g., "New Adult Medical"
	Facility  string `json:"facility,omitempty"`   // e.g., "Abita Eye Group Spring Hill"
	Confirmed bool   `json:"confirmed"`            // Whether the appointment has been confirmed
}

// HandleGetPatientAppointments retrieves appointments for a verified patient.
func (h *Handlers) HandleGetPatientAppointments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req GetAppointmentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:  "error",
			Message: "Invalid JSON body",
		})
		return
	}

	if req.PatientID == "" {
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:  "error",
			Message: "patientId is required",
		})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	log.Printf("patient-appointments: patientId=%s office=%s", req.PatientID, office.ID)

	if _, err := strconv.Atoi(req.PatientID); err != nil {
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:  "error",
			Message: "patientId must be numeric",
		})
		return
	}

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	details, err := h.fetchUpcomingAppointments(r.Context(), tokenData, req.PatientID, office)
	if err != nil {
		log.Printf("patient-appointments: error: %v", err)
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:  "error",
			Message: "Failed to retrieve appointments: " + err.Error(),
		})
		return
	}

	log.Printf("patient-appointments: found %d appointments for patient %s", len(details), req.PatientID)

	if len(details) == 0 {
		json.NewEncoder(w).Encode(PatientApptResponse{
			Status:    "no_appointments",
			PatientID: req.PatientID,
			Message:   "No appointments found for this patient",
		})
		return
	}

	json.NewEncoder(w).Encode(PatientApptResponse{
		Status:       "found",
		PatientID:    req.PatientID,
		Appointments: details,
		Message:      fmt.Sprintf("Found %d appointment(s)", len(details)),
	})
}

// PatientLookupRequest is the JSON body for the combined patient lookup endpoint.
type PatientLookupRequest struct {
	Phone  string `json:"phone"`
	DOB    string `json:"dob,omitempty"`
	Office string `json:"office,omitempty"`
}

// PatientLookupResponse is the combined response with identity + appointments.
type PatientLookupResponse struct {
	Status             string            `json:"status"`
	PatientID          string            `json:"patientId,omitempty"`
	Name               string            `json:"name,omitempty"`
	DOB                string            `json:"dob,omitempty"`
	Phone              string            `json:"phone,omitempty"`
	InsuranceCarrier   string            `json:"insuranceCarrier,omitempty"`
	InsuranceCarrierID string            `json:"insuranceCarrierId,omitempty"`
	Routing            string            `json:"routing,omitempty"`
	AllowedProviders   []string          `json:"allowedProviders,omitempty"`
	RoutingAmbiguous   bool              `json:"routingAmbiguous,omitempty"`
	Appointments       []PatientApptDetail `json:"appointments,omitempty"`
	Matches            []PatientMatch    `json:"matches,omitempty"`
	Message            string            `json:"message,omitempty"`
}

// HandlePatientLookup verifies a patient and returns their upcoming appointments in one call.
func (h *Handlers) HandlePatientLookup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req PatientLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(PatientLookupResponse{Status: "error", Message: "Invalid JSON body"})
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(PatientLookupResponse{Status: "error", Message: err.Error()})
		return
	}

	if req.Phone == "" {
		json.NewEncoder(w).Encode(PatientLookupResponse{Status: "error", Message: "phone is required"})
		return
	}

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(PatientLookupResponse{Status: "error", Message: "Failed to get authentication token: " + err.Error()})
		return
	}

	// Lookup patient by phone
	digits := domain.NormalizePhoneDigits(req.Phone)
	patients, err := h.amdClient.LookupPatientByPhone(r.Context(), tokenData, digits)
	if err != nil {
		json.NewEncoder(w).Encode(PatientLookupResponse{Status: "error", Message: "Failed to lookup patient by phone: " + err.Error()})
		return
	}
	log.Printf("patient-lookup: phone=%q returned %d patients", digits, len(patients))

	// Filter by DOB if provided
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

	// Resolve single patient
	var patient domain.Patient
	switch len(matching) {
	case 0:
		json.NewEncoder(w).Encode(PatientLookupResponse{Status: "not_found", Message: "No patient found for that phone number"})
		return
	case 1:
		patient = matching[0]
	default:
		// Multiple matches — return list for the agent to disambiguate
		var matches []PatientMatch
		for _, p := range matching {
			matches = append(matches, PatientMatch{FirstName: p.FirstName})
		}
		json.NewEncoder(w).Encode(PatientLookupResponse{
			Status:  "multiple_matches",
			Message: fmt.Sprintf("Found %d patients for this phone number. Ask the caller to confirm their name.", len(matching)),
			Matches: matches,
		})
		return
	}

	// Build response with patient identity + insurance routing
	resp := PatientLookupResponse{
		Status:    "verified",
		PatientID: patient.ID,
		Name:      patient.FullName,
		DOB:       patient.DOB,
		Phone:     patient.Phone,
	}

	demoResult, err := h.amdClient.GetDemographic(r.Context(), tokenData, patient.ID)
	if err != nil {
		log.Printf("WARNING: patient-lookup: failed to get demographics for %s: %v", patient.ID, err)
	}

	if demoResult != nil {
		if demoResult.CarrierName != "" {
			resp.InsuranceCarrier = demoResult.CarrierName
		}
		if demoResult.CarrierID != "" {
			resp.InsuranceCarrierID = demoResult.CarrierID
			routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
			resp.Routing = string(routing)
			resp.AllowedProviders = office.ProvidersForRouting(routing)
			resp.RoutingAmbiguous = ambiguous
		}
	}

	// Pediatric override
	if domain.IsMinor(patient.DOB) && resp.Routing != "" && resp.Routing != string(domain.RoutingNotAccepted) {
		resp.Routing = string(office.PediatricRouting)
		resp.AllowedProviders = office.ProvidersForRouting(office.PediatricRouting)
		resp.RoutingAmbiguous = false
	}

	// Fetch appointments
	appts, err := h.fetchUpcomingAppointments(r.Context(), tokenData, patient.ID, office)
	if err != nil {
		log.Printf("WARNING: patient-lookup: failed to get appointments for %s: %v", patient.ID, err)
		// Still return patient info — appointments are best-effort
	} else {
		resp.Appointments = appts
	}

	if len(resp.Appointments) > 0 {
		resp.Message = fmt.Sprintf("Patient verified with %d appointment(s)", len(resp.Appointments))
	} else {
		resp.Message = "Patient verified, no appointments found"
	}

	json.NewEncoder(w).Encode(resp)
}

// fetchUpcomingAppointments retrieves appointments for a patient ID (1 month back + current month + 5 months forward).
func (h *Handlers) fetchUpcomingAppointments(ctx context.Context, tokenData *domain.TokenData, patientID string, office *domain.OfficeConfig) ([]PatientApptDetail, error) {
	patientIDInt, err := strconv.Atoi(patientID)
	if err != nil {
		return nil, fmt.Errorf("patientId must be numeric: %w", err)
	}

	columnIDStr := strings.Join(office.AllowedColumnIDs(), "-")

	now := time.Now().In(eastern)
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, eastern)

	months := make([]time.Time, 7)
	months[0] = thisMonth.AddDate(0, -1, 0)
	for i := 1; i < 7; i++ {
		months[i] = thisMonth.AddDate(0, i-1, 0)
	}

	type monthResult struct {
		appts []clients.AMDAppointmentResponse
		err   error
	}
	ch := make(chan monthResult, 7)

	for _, m := range months {
		m := m
		go func() {
			appts, err := h.amdRestClient.GetAppointmentsByMonth(ctx, tokenData, columnIDStr, m.Format("2006-01-02"))
			ch <- monthResult{appts, err}
		}()
	}

	var allAppts []clients.AMDAppointmentResponse
	for range 7 {
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

		typeName := ""
		if len(a.AppointmentTypes) > 0 {
			if name, ok := office.AppointmentTypeName(a.AppointmentTypes[0]); ok {
				typeName = name
			}
		}

		details = append(details, PatientApptDetail{
			ID:        a.ID,
			Date:      startTime.Format("Monday, January 2, 2006"),
			Time:      startTime.Format("3:04 PM"),
			Provider:  office.FriendlyProviderName(a.Provider),
			Type:      typeName,
			Facility:  friendlyFacilityName(a.Facility),
			Confirmed: a.ConfirmDate != nil,
		})
	}

	return details, nil
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
	AppointmentID int `json:"appointmentId"`
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

	log.Printf("cancel-appointment: appointmentId=%d", req.AppointmentID)

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
	ColumnID          int    `json:"columnId"`
	ProfileID         int    `json:"profileId"`
	StartDatetime     string `json:"startDatetime"`
	Duration          int    `json:"duration"`
	AppointmentTypeID int    `json:"appointmentTypeId"`
	Office            string `json:"office,omitempty"`
}

// BookAppointmentResponse is returned after booking an appointment.
type BookAppointmentResponse struct {
	Status        string `json:"status"`
	AppointmentID int    `json:"appointmentId,omitempty"`
	Message       string `json:"message"`
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

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	log.Printf("book-appointment: patientId=%s columnId=%d profileId=%d startDatetime=%s duration=%d typeId=%d office=%s",
		req.PatientID, req.ColumnID, req.ProfileID, req.StartDatetime, req.Duration, req.AppointmentTypeID, office.ID)

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
	if req.AppointmentTypeID == 0 {
		json.NewEncoder(w).Encode(BookAppointmentResponse{Status: "error", Message: "appointmentTypeId is required"})
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

	// Resolve appointment type ID for current environment (prod IDs → env IDs)
	envTypeID, ok := domain.ResolveAppointmentTypeID(req.AppointmentTypeID)
	if !ok {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: fmt.Sprintf("Invalid appointment type ID: %d. Valid types: 1004 (New Pediatric), 1005 (Established Pediatric), 1006 (New Adult), 1007 (Established Adult), 1008 (Post Op)", req.AppointmentTypeID),
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

	// Parse patient ID
	patientIDInt, err := strconv.Atoi(req.PatientID)
	if err != nil {
		json.NewEncoder(w).Encode(BookAppointmentResponse{
			Status:  "error",
			Message: "patientId must be numeric",
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
	})
	if err != nil {
		log.Printf("book-appointment: AMD error: %v", err)

		// Handle 409 conflict errors with clear messages
		errStr := err.Error()
		if strings.Contains(errStr, "conflict") {
			json.NewEncoder(w).Encode(BookAppointmentResponse{
				Status:  "error",
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

	log.Printf("book-appointment: success appointmentId=%d", apptID)

	json.NewEncoder(w).Encode(BookAppointmentResponse{
		Status:        "booked",
		AppointmentID: apptID,
		Message:       "Appointment booked successfully",
	})
}

// AvailabilityRequest is the expected JSON body for availability lookup.
type AvailabilityRequest struct {
	Date            string `json:"date"`            // Required: YYYY-MM-DD format
	Provider        string `json:"provider"`        // Optional: filter by provider name
	Office          string `json:"office"`          // Optional: filter by office name (e.g., "Spring Hill", "Hollywood")
	Routing         string `json:"routing"`         // Optional: routing rule from verify/add-patient (e.g., "bach_only")
	PreauthRequired bool   `json:"preauthRequired"` // Optional: if true, enforces 14-day minimum lead time
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

	// Parse start date
	startDate, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{Status: "error", Message: "Invalid date format. Use YYYY-MM-DD."})
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

	// Resolve office config
	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: err.Error(),
		})
		return
	}

	log.Printf("availability: date=%s provider=%q office=%s routing=%q preauthRequired=%v", req.Date, req.Provider, office.ID, req.Routing, req.PreauthRequired)

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Status:  "error",
			Message: "Failed to get authentication token: " + err.Error(),
		})
		return
	}

	// Get scheduler setup (1 XMLRPC call)
	setup, err := h.amdClient.GetSchedulerSetup(r.Context(), tokenData)
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

	// Apply routing filter (insurance-based provider restriction)
	if req.Routing != "" {
		rule := domain.ParseRoutingRule(req.Routing)
		routingColumns := office.ColumnsForRouting(rule)
		if routingColumns != nil {
			var filtered []domain.SchedulerColumn
			for _, col := range allowedColumns {
				if routingColumns[col.ID] {
					filtered = append(filtered, col)
				}
			}
			allowedColumns = filtered
		} else {
			// RoutingNotAccepted — no columns allowed
			allowedColumns = nil
		}
	}

	// Determine location name for response
	locationName := office.DisplayName
	if len(allowedColumns) > 0 {
		if fac, ok := facilityMap[allowedColumns[0].FacilityID]; ok {
			locationName = fac.Name
		}
	}

	if len(allowedColumns) == 0 {
		if req.Provider != "" {
			json.NewEncoder(w).Encode(ErrorResponse{
				Status:  "error",
				Message: fmt.Sprintf("No provider found matching %q. Valid providers: %s",
					req.Provider, strings.Join(office.ValidProviderNames(), ", ")),
			})
			return
		}
		json.NewEncoder(w).Encode(domain.AvailabilityResponse{
			SearchedDate: req.Date,
			Date:         startDate.Format("Monday, January 2, 2006"),
			Location:     locationName,
			Providers:    []domain.ProviderAvailability{},
		})
		return
	}

	nowEastern := time.Now().In(eastern)

	// Try the requested date first, then auto-search forward up to 14 days
	searchDate := startDate
	var providers []domain.ProviderAvailability

	maxDate := startDate.AddDate(0, 0, 14)
	for !searchDate.After(maxDate) {
		dateStr := searchDate.Format("2006-01-02")

		// Only fetch columns that work this weekday — skip non-working providers
		var workingColumnIDs []string
		for _, col := range allowedColumns {
			if col.WorksOnDay(searchDate.Weekday()) {
				workingColumnIDs = append(workingColumnIDs, col.ID)
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
			// Skip columns where appointment data couldn't be fetched —
			// safer to omit than to show all slots as available
			if _, ok := appointmentsByColumn[col.ID]; !ok {
				log.Printf("availability: skipping column %s — appointment data unavailable", col.ID)
				continue
			}
			profile := profileMap[col.ProfileID]
			facility := facilityMap[col.FacilityID]

			displayName := office.ProviderDisplayName(col.ProfileID)
			if displayName == "" {
				displayName = profile.Name
			}

			allSlots := calculateAvailableSlots(col, appointmentsByColumn[col.ID], blockHoldsByColumn[col.ID], searchDate, nowEastern)

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
		json.NewEncoder(w).Encode(domain.AvailabilityResponse{
			SearchedDate: req.Date,
			Date:         "",
			Location:     locationName,
			Message:      "No availability found within 14 days of requested date",
			Providers:    []domain.ProviderAvailability{},
		})
		return
	}

	json.NewEncoder(w).Encode(domain.AvailabilityResponse{
		SearchedDate: req.Date,
		Date:         searchDate.Format("Monday, January 2, 2006"),
		Location:     locationName,
		Providers:    providers,
	})
}

// calculateAvailableSlots generates available time slots for a column on a single day.
// nowEastern is used to filter out past slots when the date is today.
func calculateAvailableSlots(col domain.SchedulerColumn, appointments []domain.Appointment, blockHolds []domain.BlockHold, date time.Time, nowEastern time.Time) []domain.AvailableSlot {
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

	maxAppts := col.MaxApptsPerSlot

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

		// AMD 4101: Block if any existing appointment overlaps this slot's full booking range
		if hasOverlappingAppointment(slotTime, interval, appointments) {
			continue
		}

		// AMD 4186: Check same-start-time appointment count against maxApptsPerSlot
		if maxAppts > 0 {
			count := countSameStartAppointments(slotTime, appointments)
			if count >= maxAppts {
				continue
			}
		}

		slots = append(slots, domain.AvailableSlot{
			Time:     domain.FormatSlotTime(slotTime),
			DateTime: domain.FormatSlotDateTime(slotTime),
		})
	}

	return slots
}

// hasOverlappingAppointment checks if any existing appointment overlaps with the
// full booking range [slotTime, slotTime+slotDuration). AMD returns error 4101
// for ANY overlap between the new appointment's range and an existing appointment,
// not just when the new start falls inside an existing range. This matters when
// off-grid appointments exist (e.g., a 15-min appointment at 8:45 from when the
// column had 15-min intervals blocks a 30-min booking at 8:30).
func hasOverlappingAppointment(slotTime time.Time, slotDuration time.Duration, appointments []domain.Appointment) bool {
	slotEnd := slotTime.Add(slotDuration)
	for _, appt := range appointments {
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
	InsPlanID      string `json:"insPlanId"`
	RespPartyID    string `json:"respPartyId"`
	OldInsurance   string `json:"oldInsurance"`
	Insurance      string `json:"insurance"`
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
	if req.PatientID == "" || req.Insurance == "" || req.SubscriberNum == "" {
		json.NewEncoder(w).Encode(UpdateInsuranceResponse{
			Status:  "error",
			Message: "patientId, insurance, and subscriberNum are required",
		})
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

	// Look up new insurance
	insEntry, found := domain.LookupInsurance(req.Insurance)
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
	_, ambiguous := domain.RoutingForCarrierID(insEntry.CarrierID)

	json.NewEncoder(w).Encode(UpdateInsuranceResponse{
		Status:           "updated",
		PatientID:        req.PatientID,
		OldInsurance:     req.OldInsurance,
		NewInsurance:     req.Insurance,
		Routing:          string(routing),
		AllowedProviders: office.ProvidersForRouting(routing),
		RoutingAmbiguous: ambiguous,
		PreauthRequired:  insEntry.PreauthRequired,
		Message:          "Insurance updated successfully",
	})
}
