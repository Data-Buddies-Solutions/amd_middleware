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
	Type             string                 `json:"type"`
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

// PatientMatch provides minimal info for disambiguation.
type PatientMatch struct {
	FirstName string `json:"firstName"`
}

// Handlers holds the dependencies for HTTP handlers.
type Handlers struct {
	tokenManager       *auth.TokenManager
	amdClient          *clients.AdvancedMDClient
	amdRestClient      *clients.AdvancedMDRestClient
	bookingTokenSecret string
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(tm *auth.TokenManager, amdClient *clients.AdvancedMDClient, amdRestClient *clients.AdvancedMDRestClient, bookingTokenSecret string) *Handlers {
	return &Handlers{
		tokenManager:       tm,
		amdClient:          amdClient,
		amdRestClient:      amdRestClient,
		bookingTokenSecret: bookingTokenSecret,
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

func ambiguousOptionsForCarrierID(carrierID string) []string {
	seen := map[string]bool{}
	var options []string
	for name, entry := range domain.InsuranceNameMap {
		if entry.CarrierID != carrierID {
			continue
		}
		label := cases.Title(language.English).String(name)
		if seen[label] {
			continue
		}
		seen[label] = true
		options = append(options, label)
	}
	if len(options) == 0 {
		return nil
	}
	return options
}

func insuranceSummaryFromCarrier(carrierName, carrierID string, routing domain.RoutingRule, preauthRequired bool, routingAmbiguous bool) *InsuranceSummary {
	if carrierName == "" && carrierID == "" {
		return nil
	}
	summary := &InsuranceSummary{
		Carrier:          carrierName,
		Routing:          string(routing),
		RoutingAmbiguous: routingAmbiguous,
		PreauthRequired:  preauthRequired,
	}
	if routingAmbiguous {
		summary.AmbiguousOptions = ambiguousOptionsForCarrierID(carrierID)
	}
	return summary
}

func preauthRequiredForCarrierID(carrierID string) bool {
	if carrierID == "" {
		return false
	}

	foundAcceptedPlan := false
	for _, entry := range domain.InsuranceNameMap {
		if entry.CarrierID != carrierID || entry.Routing == domain.RoutingNotAccepted {
			continue
		}
		foundAcceptedPlan = true
		if !entry.PreauthRequired {
			return false
		}
	}

	return foundAcceptedPlan
}

func mapPatientAppointments(details []PatientApptDetail) []PatientAppointmentEntry {
	if len(details) == 0 {
		return []PatientAppointmentEntry{}
	}
	out := make([]PatientAppointmentEntry, 0, len(details))
	for _, detail := range details {
		out = append(out, PatientAppointmentEntry{
			AppointmentID: detail.ID,
			Date:          detail.Date,
			Time:          detail.Time,
			Provider:      detail.Provider,
			Type:          detail.Type,
			Location:      detail.Facility,
			IsSchedulable: true,
			Confirmed:     detail.Confirmed,
		})
	}
	return out
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

// HandleAddPatient creates a new patient in AdvancedMD and attaches insurance.
func (h *Handlers) HandleAddPatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req AddPatientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("add-patient: failed to decode JSON: %v", err)
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", AddPatientStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), AddPatientStructured{
			Outcome: "invalid_request",
		}))
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
		json.NewEncoder(w).Encode(Err(fmt.Sprintf("Missing required fields: %s.", strings.Join(missing, ", ")), AddPatientStructured{
			Outcome: "invalid_request",
		}))
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
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", AddPatientStructured{
			Outcome: "dependency_failure",
		}))
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
			json.NewEncoder(w).Encode(Err("A patient with this name and date of birth already exists in the system. Verify the patient instead of registering again.", AddPatientStructured{
				Outcome: "duplicate_patient",
			}))
			return
		}
		json.NewEncoder(w).Encode(Err("AdvancedMD could not create the patient record. Try again in a moment.", AddPatientStructured{
			Outcome: "dependency_failure",
		}))
		return
	}

	strippedID := domain.StripPatientPrefix(rawPatientID)

	// Look up insurance entry from name
	insEntry, ok := domain.LookupInsurance(req.Insurance)
	if !ok {
		json.NewEncoder(w).Encode(OK(fmt.Sprintf("Patient created, but the insurance %q was not recognized. Use a plan name from the accepted list before scheduling.", req.Insurance), AddPatientStructured{
			Outcome:   "created_unrecognized_insurance",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
		}))
		return
	}

	// Reject insurance not accepted at this office
	if insEntry.Routing == domain.RoutingNotAccepted {
		json.NewEncoder(w).Encode(OK(fmt.Sprintf("Patient created, but %s is not accepted at %s. Do not schedule under this plan.", req.Insurance, office.DisplayName), AddPatientStructured{
			Outcome:   "created_not_accepted",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
			Insurance: insuranceSummaryFromCarrier(
				req.Insurance,
				insEntry.CarrierID,
				insEntry.Routing,
				insEntry.PreauthRequired,
				false,
			),
		}))
		return
	}

	// Attach insurance
	if err := h.amdClient.AddInsurance(r.Context(), tokenData, rawPatientID, respPartyID, insEntry.CarrierID, req.SubscriberNum); err != nil {
		json.NewEncoder(w).Encode(OK("Patient created, but AdvancedMD could not attach the insurance record. Do not schedule until insurance is fixed.", AddPatientStructured{
			Outcome:   "created_without_insurance",
			PatientID: strippedID,
			Name:      patientName,
			DOB:       normalizedDOB,
		}))
		return
	}

	// Pediatric override: under-18 patients → office pediatric routing
	routing := insEntry.Routing
	if domain.IsMinor(normalizedDOB) && routing != domain.RoutingNotAccepted {
		routing = office.PediatricRouting
	}
	insurance := insuranceSummaryFromCarrier(req.Insurance, insEntry.CarrierID, routing, insEntry.PreauthRequired, false)

	json.NewEncoder(w).Encode(OK("Patient created and insurance attached successfully.", AddPatientStructured{
		Outcome:          "created",
		PatientID:        strippedID,
		Name:             patientName,
		DOB:              normalizedDOB,
		Insurance:        insurance,
		AllowedProviders: office.ProvidersForRouting(routing),
	}))
}

// HandleVerifyPatient looks up a patient by name and DOB.
func (h *Handlers) HandleVerifyPatient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse request body
	var req VerifyPatientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", VerifyPatientStructured{Outcome: "invalid_request"}))
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), VerifyPatientStructured{Outcome: "invalid_request"}))
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
		json.NewEncoder(w).Encode(Err("Provide phone plus first name, phone plus date of birth, or last name plus date of birth.", VerifyPatientStructured{
			Outcome: "invalid_request",
		}))
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
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment, or transfer if it persists.", VerifyPatientStructured{
			Outcome: "dependency_failure",
		}))
		return
	}

	// Call AdvancedMD lookuppatient API — by phone or by name
	var patients []domain.Patient
	if hasPhone {
		digits := domain.NormalizePhoneDigits(req.Phone)
		patients, err = h.amdClient.LookupPatientByPhone(r.Context(), tokenData, digits)
		if err != nil {
			json.NewEncoder(w).Encode(Err("AdvancedMD is temporarily unreachable while looking up the caller. Try again in a moment, or transfer if it persists.", VerifyPatientStructured{
				Outcome: "dependency_failure",
			}))
			return
		}
		log.Printf("verify-patient: lookup phone=%q returned %d patients", digits, len(patients))
	} else {
		normalizedLastName := domain.StripDiacritics(req.LastName)
		normalizedFirstName := domain.StripDiacritics(req.FirstName)
		patients, err = h.amdClient.LookupPatient(r.Context(), tokenData, normalizedLastName, normalizedFirstName)
		if err != nil {
			json.NewEncoder(w).Encode(Err("AdvancedMD is temporarily unreachable while looking up the patient. Try again in a moment, or transfer if it persists.", VerifyPatientStructured{
				Outcome: "dependency_failure",
			}))
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
		json.NewEncoder(w).Encode(OK("No patient matched the provided information. If the spelling is right, offer to register them as a new patient.", VerifyPatientStructured{
			Outcome: "not_found",
		}))
		return

	case 1:
		p := matchingPatients[0]
		demoResult, err := h.amdClient.GetDemographic(r.Context(), tokenData, p.ID)
		if err != nil {
			log.Printf("WARNING: failed to get demographics for patient %s: %v", p.ID, err)
		}

		var insurance *InsuranceSummary
		var allowedProviders []string
		pediatricOverride := false
		if demoResult != nil {
			if demoResult.CarrierID != "" {
				routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
				allowedProviders = office.ProvidersForRouting(routing)
				insurance = insuranceSummaryFromCarrier(
					demoResult.CarrierName,
					demoResult.CarrierID,
					routing,
					preauthRequiredForCarrierID(demoResult.CarrierID),
					ambiguous,
				)
			}
		}

		// Pediatric override: under-18 patients → office pediatric routing
		if domain.IsMinor(p.DOB) && insurance != nil && insurance.Routing != string(domain.RoutingNotAccepted) {
			pediatricOverride = true
			insurance.Routing = string(office.PediatricRouting)
			insurance.RoutingAmbiguous = false
			insurance.AmbiguousOptions = nil
			allowedProviders = office.ProvidersForRouting(office.PediatricRouting)
		}

		summary := fmt.Sprintf("Patient verified: %s, DOB %s.", p.FullName, p.DOB)
		if insurance != nil {
			summary = fmt.Sprintf("Patient verified: %s, DOB %s, %s.", p.FullName, p.DOB, insurance.Carrier)
			if insurance.RoutingAmbiguous {
				summary = fmt.Sprintf("Patient verified: %s, %s. Ask the caller which plan variation they have before scheduling.", p.FullName, insurance.Carrier)
			}
		}
		if pediatricOverride {
			summary = fmt.Sprintf("Patient verified: %s, DOB %s. Routing is restricted to pediatric scheduling because the patient is under 18.", p.FullName, p.DOB)
		}

		json.NewEncoder(w).Encode(OK(summary, VerifyPatientStructured{
			Outcome:          "verified",
			PatientID:        p.ID,
			Name:             p.FullName,
			DOB:              p.DOB,
			Phone:            p.Phone,
			Insurance:        insurance,
			AllowedProviders: allowedProviders,
		}))
		return

	default:
		if !hasDOB {
			// Phone + firstName path: first name already used as filter, ask for DOB to disambiguate
			json.NewEncoder(w).Encode(OK("Multiple patients match that phone number and first name. Ask for date of birth to narrow it down.", VerifyPatientStructured{
				Outcome:        "multiple_matches",
				Disambiguation: "dob",
			}))
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

					var insurance *InsuranceSummary
					var allowedProviders []string
					pediatricOverride := false
					if demoResult != nil {
						if demoResult.CarrierID != "" {
							routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
							allowedProviders = office.ProvidersForRouting(routing)
							insurance = insuranceSummaryFromCarrier(
								demoResult.CarrierName,
								demoResult.CarrierID,
								routing,
								preauthRequiredForCarrierID(demoResult.CarrierID),
								ambiguous,
							)
						}
					}

					// Pediatric override: under-18 patients → office pediatric routing
					if domain.IsMinor(p.DOB) && insurance != nil && insurance.Routing != string(domain.RoutingNotAccepted) {
						pediatricOverride = true
						insurance.Routing = string(office.PediatricRouting)
						insurance.RoutingAmbiguous = false
						insurance.AmbiguousOptions = nil
						allowedProviders = office.ProvidersForRouting(office.PediatricRouting)
					}

					summary := fmt.Sprintf("Patient verified: %s, DOB %s.", p.FullName, p.DOB)
					if insurance != nil {
						summary = fmt.Sprintf("Patient verified: %s, DOB %s, %s.", p.FullName, p.DOB, insurance.Carrier)
						if insurance.RoutingAmbiguous {
							summary = fmt.Sprintf("Patient verified: %s, %s. Ask the caller which plan variation they have before scheduling.", p.FullName, insurance.Carrier)
						}
					}
					if pediatricOverride {
						summary = fmt.Sprintf("Patient verified: %s, DOB %s. Routing is restricted to pediatric scheduling because the patient is under 18.", p.FullName, p.DOB)
					}

					json.NewEncoder(w).Encode(OK(summary, VerifyPatientStructured{
						Outcome:          "verified",
						PatientID:        p.ID,
						Name:             p.FullName,
						DOB:              p.DOB,
						Phone:            p.Phone,
						Insurance:        insurance,
						AllowedProviders: allowedProviders,
					}))
					return
				}
			}
			json.NewEncoder(w).Encode(OK("No patient matched that first name with the supplied date of birth.", VerifyPatientStructured{
				Outcome: "not_found",
			}))
			return
		}

		// Return list of first names for disambiguation
		var matches []PatientMatch
		for _, p := range matchingPatients {
			matches = append(matches, PatientMatch{FirstName: p.FirstName})
		}
		json.NewEncoder(w).Encode(OK("Multiple patients match that last name and date of birth. Ask for the patient's first name.", VerifyPatientStructured{
			Outcome:        "multiple_matches",
			Disambiguation: "first_name",
			Matches:        matches,
		}))
	}
}

// GetAppointmentsRequest is the expected JSON body for patient appointment lookup.
type GetAppointmentsRequest struct {
	PatientID string `json:"patientId"`
	Office    string `json:"office,omitempty"`
}

// PatientApptDetail is a single appointment formatted for LLM consumption.
type PatientApptDetail struct {
	ID        int    `json:"id"`                 // AMD appointment ID — for cancel_appt
	Date      string `json:"date"`               // Human-readable (e.g., "Wednesday, March 18, 2026")
	Time      string `json:"time"`               // e.g., "12:00 PM"
	Provider  string `json:"provider,omitempty"` // e.g., "Dr. Austin Bach"
	Type      string `json:"type,omitempty"`     // e.g., "New Adult Medical"
	Facility  string `json:"facility,omitempty"` // e.g., "Abita Eye Group Spring Hill"
	Confirmed bool   `json:"confirmed"`          // Whether the appointment has been confirmed
}

// HandleGetPatientAppointments retrieves appointments for a verified patient.
func (h *Handlers) HandleGetPatientAppointments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req GetAppointmentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", PatientAppointmentsStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	if req.PatientID == "" {
		json.NewEncoder(w).Encode(Err("patientId is required.", PatientAppointmentsStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), PatientAppointmentsStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	log.Printf("patient-appointments: patientId=%s office=%s", req.PatientID, office.ID)

	if _, err := strconv.Atoi(req.PatientID); err != nil {
		json.NewEncoder(w).Encode(Err("patientId must be numeric.", PatientAppointmentsStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", PatientAppointmentsStructured{
			Outcome: "dependency_failure",
		}))
		return
	}

	details, err := h.fetchUpcomingAppointments(r.Context(), tokenData, req.PatientID, office)
	if err != nil {
		log.Printf("patient-appointments: error: %v", err)
		json.NewEncoder(w).Encode(Err("AdvancedMD could not retrieve appointments right now. Try again in a moment.", PatientAppointmentsStructured{
			Outcome:   "dependency_failure",
			PatientID: req.PatientID,
		}))
		return
	}

	log.Printf("patient-appointments: found %d appointments for patient %s", len(details), req.PatientID)

	if len(details) == 0 {
		json.NewEncoder(w).Encode(OK("No appointments found for this patient.", PatientAppointmentsStructured{
			Outcome:   "no_appointments",
			PatientID: req.PatientID,
		}))
		return
	}

	json.NewEncoder(w).Encode(OK(fmt.Sprintf("Found %d appointment(s).", len(details)), PatientAppointmentsStructured{
		Outcome:      "found",
		PatientID:    req.PatientID,
		Appointments: mapPatientAppointments(details),
	}))
}

// PatientLookupRequest is the JSON body for the combined patient lookup endpoint.
type PatientLookupRequest struct {
	Phone  string `json:"phone"`
	DOB    string `json:"dob,omitempty"`
	Office string `json:"office,omitempty"`
}

// HandlePatientLookup verifies a patient and returns their upcoming appointments in one call.
func (h *Handlers) HandlePatientLookup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req PatientLookupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", PatientLookupStructured{Outcome: "invalid_request"}))
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), PatientLookupStructured{Outcome: "invalid_request"}))
		return
	}

	if req.Phone == "" {
		json.NewEncoder(w).Encode(Err("phone is required", PatientLookupStructured{Outcome: "invalid_request"}))
		return
	}

	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", PatientLookupStructured{
			Outcome: "dependency_failure",
		}))
		return
	}

	// Lookup patient by phone
	digits := domain.NormalizePhoneDigits(req.Phone)
	patients, err := h.amdClient.LookupPatientByPhone(r.Context(), tokenData, digits)
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD is temporarily unreachable while looking up the caller.", PatientLookupStructured{
			Outcome: "dependency_failure",
		}))
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
		json.NewEncoder(w).Encode(OK("No patient matched that phone number.", PatientLookupStructured{
			Outcome: "not_found",
		}))
		return
	case 1:
		patient = matching[0]
	default:
		// Multiple matches — return list for the agent to disambiguate
		var matches []PatientMatch
		for _, p := range matching {
			matches = append(matches, PatientMatch{FirstName: p.FirstName})
		}
		json.NewEncoder(w).Encode(OK("Multiple patients match this phone number. Ask the caller for the patient's first name.", PatientLookupStructured{
			Outcome: "multiple_matches",
			Matches: matches,
		}))
		return
	}

	var insurance *InsuranceSummary
	var allowedProviders []string
	pediatricOverride := false
	demoResult, err := h.amdClient.GetDemographic(r.Context(), tokenData, patient.ID)
	if err != nil {
		log.Printf("WARNING: patient-lookup: failed to get demographics for %s: %v", patient.ID, err)
	}

	if demoResult != nil {
		if demoResult.CarrierID != "" {
			routing, ambiguous := domain.RoutingForCarrierID(demoResult.CarrierID)
			allowedProviders = office.ProvidersForRouting(routing)
			insurance = insuranceSummaryFromCarrier(
				demoResult.CarrierName,
				demoResult.CarrierID,
				routing,
				preauthRequiredForCarrierID(demoResult.CarrierID),
				ambiguous,
			)
		}
	}

	// Pediatric override
	if domain.IsMinor(patient.DOB) && insurance != nil && insurance.Routing != string(domain.RoutingNotAccepted) {
		pediatricOverride = true
		insurance.Routing = string(office.PediatricRouting)
		insurance.RoutingAmbiguous = false
		insurance.AmbiguousOptions = nil
		allowedProviders = office.ProvidersForRouting(office.PediatricRouting)
	}

	// Fetch appointments
	appts, err := h.fetchUpcomingAppointments(r.Context(), tokenData, patient.ID, office)
	if err != nil {
		log.Printf("WARNING: patient-lookup: failed to get appointments for %s: %v", patient.ID, err)
		// Still return patient info — appointments are best-effort
	}
	appointments := mapPatientAppointments(appts)

	summary := fmt.Sprintf("Patient verified: %s.", patient.FullName)
	if len(appointments) > 0 {
		summary = fmt.Sprintf("Patient verified: %s. %d appointment(s) found.", patient.FullName, len(appointments))
	} else {
		summary = fmt.Sprintf("Patient verified: %s. No appointments found.", patient.FullName)
	}
	if pediatricOverride {
		summary = fmt.Sprintf("Patient verified: %s. Pediatric routing is active because the patient is under 18.", patient.FullName)
	}

	json.NewEncoder(w).Encode(OK(summary, PatientLookupStructured{
		Outcome:          "verified",
		PatientID:        patient.ID,
		Name:             patient.FullName,
		DOB:              patient.DOB,
		Phone:            patient.Phone,
		Insurance:        insurance,
		AllowedProviders: allowedProviders,
		Appointments:     appointments,
	}))
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

// HandleCancelAppointment cancels an appointment in AdvancedMD.
func (h *Handlers) HandleCancelAppointment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req CancelAppointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", CancelAppointmentStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	if req.AppointmentID == 0 {
		json.NewEncoder(w).Encode(Err("appointmentId is required.", CancelAppointmentStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	log.Printf("cancel-appointment: appointmentId=%d", req.AppointmentID)

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", CancelAppointmentStructured{
			Outcome: "dependency_failure",
		}))
		return
	}

	// Cancel via AMD REST API
	if err := h.amdRestClient.CancelAppointment(r.Context(), tokenData, req.AppointmentID); err != nil {
		log.Printf("cancel-appointment: AMD error: %v", err)
		json.NewEncoder(w).Encode(Err("AdvancedMD could not cancel the appointment right now. Try again in a moment.", CancelAppointmentStructured{
			Outcome:       "dependency_failure",
			AppointmentID: req.AppointmentID,
		}))
		return
	}

	json.NewEncoder(w).Encode(OK("Appointment cancelled successfully.", CancelAppointmentStructured{
		Outcome:       "cancelled",
		AppointmentID: req.AppointmentID,
	}))
}

// BookAppointmentRequest is the expected JSON body for booking an appointment.
type BookAppointmentRequest struct {
	PatientID         string `json:"patientId"`
	BookingToken      string `json:"bookingToken,omitempty"`
	ColumnID          int    `json:"columnId,omitempty"`
	ProfileID         int    `json:"profileId,omitempty"`
	StartDatetime     string `json:"startDatetime,omitempty"`
	Duration          int    `json:"duration,omitempty"`
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
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}

	if req.PatientID == "" {
		json.NewEncoder(w).Encode(Err("patientId is required.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}
	if req.AppointmentTypeID == 0 {
		json.NewEncoder(w).Encode(Err("appointmentTypeId is required.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}

	var tokenPayload *bookingTokenPayload
	if req.BookingToken != "" {
		payload, err := h.parseBookingToken(req.BookingToken)
		if err != nil {
			json.NewEncoder(w).Encode(Err("That booking token is no longer valid. Call availability again to get a fresh slot.", BookAppointmentStructured{
				Outcome: "stale_booking_token",
			}))
			return
		}
		tokenPayload = payload
		req.ColumnID = payload.ColumnID
		req.ProfileID = payload.ProfileID
		req.StartDatetime = payload.StartDatetime
		req.Duration = payload.Duration
		if req.Office == "" {
			req.Office = payload.Office
		}
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}

	log.Printf("book-appointment: patientId=%s columnId=%d profileId=%d startDatetime=%s duration=%d typeId=%d office=%s",
		req.PatientID, req.ColumnID, req.ProfileID, req.StartDatetime, req.Duration, req.AppointmentTypeID, office.ID)

	// Validate required fields
	if req.ColumnID == 0 {
		json.NewEncoder(w).Encode(Err("bookingToken or columnId is required.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}
	if req.ProfileID == 0 {
		json.NewEncoder(w).Encode(Err("profileId is required.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}
	if req.StartDatetime == "" {
		json.NewEncoder(w).Encode(Err("startDatetime is required.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}
	if req.Duration == 0 {
		json.NewEncoder(w).Encode(Err("duration is required.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}

	// Validate columnId is allowed for this office
	colIDStr := strconv.Itoa(req.ColumnID)
	if !office.IsAllowedColumn(colIDStr) {
		json.NewEncoder(w).Encode(Err(fmt.Sprintf("That slot is not valid for %s.", office.DisplayName), BookAppointmentStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	// Resolve appointment type ID for current environment (prod IDs → env IDs)
	envTypeID, ok := domain.ResolveAppointmentTypeID(req.AppointmentTypeID)
	if !ok {
		json.NewEncoder(w).Encode(Err("Invalid appointmentTypeId.", BookAppointmentStructured{
			Outcome:                 "invalid_appointment_type",
			ValidAppointmentTypeIDs: []int{1004, 1005, 1006, 1007, 1008},
		}))
		return
	}

	// Resolve color from canonical (prod) type ID
	color, ok := office.AppointmentColor(req.AppointmentTypeID)
	if !ok {
		json.NewEncoder(w).Encode(Err("Invalid appointmentTypeId.", BookAppointmentStructured{
			Outcome:                 "invalid_appointment_type",
			ValidAppointmentTypeIDs: []int{1004, 1005, 1006, 1007, 1008},
		}))
		return
	}

	// Parse patient ID
	patientIDInt, err := strconv.Atoi(req.PatientID)
	if err != nil {
		json.NewEncoder(w).Encode(Err("patientId must be numeric.", BookAppointmentStructured{Outcome: "invalid_request"}))
		return
	}

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", BookAppointmentStructured{
			Outcome: "dependency_failure",
		}))
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
			json.NewEncoder(w).Encode(Err("That slot was just taken. Check availability again and offer a different time.", BookAppointmentStructured{
				Outcome: "slot_taken",
			}))
			return
		}

		json.NewEncoder(w).Encode(Err("AdvancedMD could not complete the booking. Try again in a moment, or offer another slot.", BookAppointmentStructured{
			Outcome: "dependency_failure",
		}))
		return
	}

	log.Printf("book-appointment: success appointmentId=%d", apptID)
	slotTime, err := time.Parse("2006-01-02T15:04", req.StartDatetime)
	if err != nil {
		slotTime = time.Time{}
	}
	providerName := office.ProviderDisplayName(strconv.Itoa(req.ProfileID))
	if providerName == "" {
		providerName = "Selected provider"
	}
	location := office.DisplayName
	if tokenPayload != nil && tokenPayload.FacilityID != "" {
		location = office.DisplayName
	}
	json.NewEncoder(w).Encode(OK("Appointment booked successfully.", BookAppointmentStructured{
		Outcome:       "booked",
		AppointmentID: apptID,
		Date:          slotTime.Format("2006-01-02"),
		Time:          slotTime.Format("3:04 PM"),
		Provider:      providerName,
		Location:      location,
	}))
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
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", AvailabilityStructured{
			Outcome: "invalid_request",
		}))
		return
	}
	originalRequestedDate := req.Date

	// Validate required date field
	if req.Date == "" {
		json.NewEncoder(w).Encode(Err("date is required in YYYY-MM-DD format.", AvailabilityStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	// Parse start date
	startDate, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		json.NewEncoder(w).Encode(Err("date must use YYYY-MM-DD format.", AvailabilityStructured{
			Outcome:      "invalid_request",
			SearchedDate: originalRequestedDate,
			Providers:    []ProviderAvailabilityResult{},
		}))
		return
	}

	nowEastern := time.Now().In(eastern)

	// Preauth: auto-advance to 14 days out if requested date is too soon.
	// This runs before same-day rejection so HMO callers asking for "today" or
	// another near-term date still get the minimum allowed booking date.
	preauthShifted := false
	if req.PreauthRequired {
		before := req.Date
		startDate, req.Date = enforcePreauthMinDate(startDate, nowEastern)
		preauthShifted = req.Date != before
	}

	// Reject same-day or past date searches when preauth did not already move the date.
	todayStr := nowEastern.Format("2006-01-02")
	if startDate.Format("2006-01-02") <= todayStr {
		json.NewEncoder(w).Encode(Err("Same-day and past-date appointments are not available. Search for tomorrow or later.", AvailabilityStructured{
			Outcome:      "same_day_disallowed",
			SearchedDate: originalRequestedDate,
			Providers:    []ProviderAvailabilityResult{},
		}))
		return
	}

	// Resolve office config
	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), AvailabilityStructured{
			Outcome:      "invalid_request",
			SearchedDate: originalRequestedDate,
			Providers:    []ProviderAvailabilityResult{},
		}))
		return
	}

	log.Printf("availability: date=%s provider=%q office=%s routing=%q preauthRequired=%v", req.Date, req.Provider, office.ID, req.Routing, req.PreauthRequired)

	// Get auth token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", AvailabilityStructured{
			Outcome:      "dependency_failure",
			SearchedDate: originalRequestedDate,
			Providers:    []ProviderAvailabilityResult{},
		}))
		return
	}

	// Get scheduler setup (1 XMLRPC call)
	setup, err := h.amdClient.GetSchedulerSetup(r.Context(), tokenData)
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD is temporarily unreachable while loading the schedule. Try again in a moment.", AvailabilityStructured{
			Outcome:      "dependency_failure",
			SearchedDate: originalRequestedDate,
			Providers:    []ProviderAvailabilityResult{},
		}))
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
			json.NewEncoder(w).Encode(Err(fmt.Sprintf("No provider matched %q. Valid providers are %s.", req.Provider, strings.Join(office.ValidProviderNames(), ", ")), AvailabilityStructured{
				Outcome:      "invalid_request",
				SearchedDate: originalRequestedDate,
				Location:     locationName,
				Providers:    []ProviderAvailabilityResult{},
			}))
			return
		}
		shiftReason := ""
		if preauthShifted {
			shiftReason = "preauth_min_lead_time"
		}
		json.NewEncoder(w).Encode(OK("No providers are available for that routing at this location.", AvailabilityStructured{
			Outcome:      "no_availability",
			SearchedDate: originalRequestedDate,
			ActualDate:   "",
			DateShifted:  preauthShifted,
			ShiftReason:  shiftReason,
			Location:     locationName,
			Providers:    []ProviderAvailabilityResult{},
		}))
		return
	}

	// Try the requested date first, then auto-search forward up to 14 days
	searchDate := startDate
	var providers []ProviderAvailabilityResult

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
			displayName := office.ProviderDisplayName(col.ProfileID)
			if displayName == "" {
				displayName = profile.Name
			}

			allSlots := calculateAvailableSlots(col, appointmentsByColumn[col.ID], blockHoldsByColumn[col.ID], searchDate, nowEastern)

			pa := ProviderAvailabilityResult{
				Name:           displayName,
				SlotDuration:   col.Interval,
				TotalAvailable: len(allSlots),
			}

			if len(allSlots) > 0 {
				pa.FirstAvailable = allSlots[0].Time
				pa.LastAvailable = allSlots[len(allSlots)-1].Time
				visibleSlots := allSlots
				if len(visibleSlots) > 5 {
					visibleSlots = visibleSlots[:5]
				}
				pa.Slots = make([]AvailableSlotResult, 0, len(visibleSlots))
				colID, _ := strconv.Atoi(col.ID)
				profID, _ := strconv.Atoi(col.ProfileID)
				for _, slot := range visibleSlots {
					token, err := h.mintBookingToken(bookingTokenPayload{
						ColumnID:      colID,
						ProfileID:     profID,
						FacilityID:    col.FacilityID,
						StartDatetime: slot.DateTime,
						Duration:      col.Interval,
						Office:        req.Office,
					})
					if err != nil {
						log.Printf("availability: failed to mint booking token: %v", err)
						continue
					}
					pa.Slots = append(pa.Slots, AvailableSlotResult{
						Time:         slot.Time,
						BookingToken: token,
					})
				}
			} else {
				pa.Slots = []AvailableSlotResult{}
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
		shiftReason := ""
		if preauthShifted {
			shiftReason = "preauth_min_lead_time"
		}
		json.NewEncoder(w).Encode(OK("No openings were found within the next two weeks.", AvailabilityStructured{
			Outcome:      "no_availability",
			SearchedDate: originalRequestedDate,
			ActualDate:   "",
			DateShifted:  preauthShifted,
			ShiftReason:  shiftReason,
			Location:     locationName,
			Providers:    []ProviderAvailabilityResult{},
		}))
		return
	}

	dateShifted := searchDate.Format("2006-01-02") != originalRequestedDate
	shiftReason := ""
	if preauthShifted {
		shiftReason = "preauth_min_lead_time"
	} else if dateShifted {
		shiftReason = "fully_booked"
	}
	summary := fmt.Sprintf("%d provider(s) have openings on %s at %s.", len(providers), searchDate.Format("January 2"), locationName)
	if dateShifted {
		summary = fmt.Sprintf("%s is unavailable. Returning openings for %s instead.", originalRequestedDate, searchDate.Format("January 2, 2006"))
		if preauthShifted {
			summary = fmt.Sprintf("Preauthorization requires a later date. Returning openings for %s.", searchDate.Format("January 2, 2006"))
		}
	}
	json.NewEncoder(w).Encode(OK(summary, AvailabilityStructured{
		Outcome:      "found",
		SearchedDate: originalRequestedDate,
		ActualDate:   searchDate.Format("2006-01-02"),
		DateShifted:  dateShifted,
		ShiftReason:  shiftReason,
		Location:     locationName,
		Providers:    providers,
	}))
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

// HandleUpdateInsurance swaps a patient's insurance: end-dates the old plan and attaches a new one.
func (h *Handlers) HandleUpdateInsurance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req UpdateInsuranceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(Err("Invalid JSON body.", UpdateInsuranceStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	// Validate required fields
	if req.PatientID == "" || req.Insurance == "" || req.SubscriberNum == "" {
		json.NewEncoder(w).Encode(Err("patientId, insurance, and subscriberNum are required.", UpdateInsuranceStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	office, err := resolveOffice(req.Office)
	if err != nil {
		json.NewEncoder(w).Encode(Err(err.Error(), UpdateInsuranceStructured{
			Outcome: "invalid_request",
		}))
		return
	}

	// Look up new insurance
	insEntry, found := domain.LookupInsurance(req.Insurance)
	if !found {
		json.NewEncoder(w).Encode(OK(fmt.Sprintf("Insurance not recognized: %q. Please use an insurance name from the accepted list.", req.Insurance), UpdateInsuranceStructured{
			Outcome:      "unrecognized_insurance",
			PatientID:    req.PatientID,
			OldInsurance: req.OldInsurance,
		}))
		return
	}

	if insEntry.Routing == domain.RoutingNotAccepted {
		json.NewEncoder(w).Encode(OK(fmt.Sprintf("%s is not accepted at %s.", req.Insurance, office.DisplayName), UpdateInsuranceStructured{
			Outcome:      "not_accepted",
			PatientID:    req.PatientID,
			OldInsurance: req.OldInsurance,
			Insurance: insuranceSummaryFromCarrier(
				req.Insurance,
				insEntry.CarrierID,
				insEntry.Routing,
				insEntry.PreauthRequired,
				false,
			),
		}))
		return
	}

	// Get AMD token
	tokenData, err := h.tokenManager.GetToken(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD authentication is temporarily unavailable. Try again in a moment.", UpdateInsuranceStructured{
			Outcome:      "dependency_failure",
			PatientID:    req.PatientID,
			OldInsurance: req.OldInsurance,
		}))
		return
	}

	// Resolve current insurance record server-side so the agent never has to carry AMD plan IDs.
	demoResult, err := h.amdClient.GetDemographic(r.Context(), tokenData, req.PatientID)
	if err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD is temporarily unreachable while retrieving the current insurance record.", UpdateInsuranceStructured{
			Outcome:      "dependency_failure",
			PatientID:    req.PatientID,
			OldInsurance: req.OldInsurance,
		}))
		return
	}
	currentInsPlanID := req.InsPlanID
	currentRespPartyID := req.RespPartyID
	if demoResult != nil {
		if demoResult.InsPlanID != "" {
			currentInsPlanID = demoResult.InsPlanID
		}
		if demoResult.RespPartyID != "" {
			currentRespPartyID = demoResult.RespPartyID
		}
	}

	// End-date old plan if a current plan ID exists.
	if currentInsPlanID != "" {
		if err := h.amdClient.EndDateInsurance(r.Context(), tokenData, req.PatientID, currentInsPlanID); err != nil {
			json.NewEncoder(w).Encode(Err("AdvancedMD could not retire the existing insurance record.", UpdateInsuranceStructured{
				Outcome:      "dependency_failure",
				PatientID:    req.PatientID,
				OldInsurance: req.OldInsurance,
			}))
			return
		}
	}

	// Add new insurance plan
	if err := h.amdClient.AddInsurance(r.Context(), tokenData, req.PatientID, currentRespPartyID, insEntry.CarrierID, req.SubscriberNum); err != nil {
		json.NewEncoder(w).Encode(Err("AdvancedMD could not add the new insurance record.", UpdateInsuranceStructured{
			Outcome:      "dependency_failure",
			PatientID:    req.PatientID,
			OldInsurance: req.OldInsurance,
		}))
		return
	}

	routing := insEntry.Routing
	_, ambiguous := domain.RoutingForCarrierID(insEntry.CarrierID)
	insurance := insuranceSummaryFromCarrier(req.Insurance, insEntry.CarrierID, routing, insEntry.PreauthRequired, ambiguous)
	allowedProviders := office.ProvidersForRouting(routing)

	if demoResult != nil && domain.IsMinor(demoResult.DOB) && insurance.Routing != string(domain.RoutingNotAccepted) {
		insurance.Routing = string(office.PediatricRouting)
		insurance.RoutingAmbiguous = false
		insurance.AmbiguousOptions = nil
		allowedProviders = office.ProvidersForRouting(office.PediatricRouting)
	}

	json.NewEncoder(w).Encode(OK(fmt.Sprintf("Insurance updated to %s.", req.Insurance), UpdateInsuranceStructured{
		Outcome:          "updated",
		PatientID:        req.PatientID,
		OldInsurance:     req.OldInsurance,
		Insurance:        insurance,
		AllowedProviders: allowedProviders,
	}))
}
