package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

// AMDLookupRequest is the XMLRPC request format for lookuppatient.
type AMDLookupRequest struct {
	PPMDMsg AMDLookupMsg `json:"ppmdmsg"`
}

// AMDLookupMsg contains the lookuppatient action parameters.
type AMDLookupMsg struct {
	Action string `json:"@action"`
	Class  string `json:"@class"`
	Name   string `json:"@name"`
}

// AMDLookupResponse represents the AdvancedMD lookuppatient response (array format).
type AMDLookupResponse struct {
	PPMDResults struct {
		Results struct {
			PatientList struct {
				ItemCount string       `json:"@itemcount"`
				Patients  []AMDPatient `json:"patient"`
			} `json:"patientlist"`
		} `json:"Results"`
		Error interface{} `json:"Error"`
	} `json:"PPMDResults"`
}

// AMDLookupResponseSingle handles single patient response.
type AMDLookupResponseSingle struct {
	PPMDResults struct {
		Results struct {
			PatientList struct {
				ItemCount string     `json:"@itemcount"`
				Patient   AMDPatient `json:"patient"`
			} `json:"patientlist"`
		} `json:"Results"`
		Error interface{} `json:"Error"`
	} `json:"PPMDResults"`
}

// AMDPatient represents a patient record from AdvancedMD.
type AMDPatient struct {
	ID          string         `json:"@id"`
	Name        string         `json:"@name"`
	DOB         string         `json:"@dob"`
	Gender      string         `json:"@gender"`
	Chart       string         `json:"@chart"`
	RespParty   string         `json:"@respparty"`
	ContactInfo AMDContactInfo `json:"contactinfo"`
}

type AMDContactInfo struct {
	HomePhone   string `json:"@homephone"`
	CellPhone   string `json:"@cellphone"`
	MobilePhone string `json:"@mobilephone"`
	WorkPhone   string `json:"@workphone"`
	OfficePhone string `json:"@officephone"`
	OtherPhone  string `json:"@otherphone"`
}

// AdvancedMDClient handles XMLRPC calls to AdvancedMD.
type AdvancedMDClient struct {
	httpClient *http.Client
}

// ProviderRejectionError means AdvancedMD explicitly rejected a request. It
// intentionally carries no provider body or patient data.
type ProviderRejectionError struct {
	operation string
}

func (e *ProviderRejectionError) Error() string {
	return e.operation + " rejected by provider"
}

// HTTPStatusError reports a provider status without retaining its body or URL.
type HTTPStatusError struct {
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected XMLRPC status %d", e.StatusCode)
}

// NewAdvancedMDClient creates a new AdvancedMD XMLRPC client.
func NewAdvancedMDClient(httpClient *http.Client) *AdvancedMDClient {
	return &AdvancedMDClient{httpClient: httpClient}
}

// doXMLRPCRequest marshals payload to JSON, POSTs to the XMLRPC endpoint, and returns the raw response body.
func (c *AdvancedMDClient) doXMLRPCRequest(ctx context.Context, tokenData *domain.TokenData, payload interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := "https://" + tokenData.XmlrpcURL

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Cookie", tokenData.CookieToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode}
	}

	return body, nil
}

// LookupPatient searches for patients by name.
// If firstName is provided, sends "LastName,FirstName" to AMD for narrower results.
func (c *AdvancedMDClient) LookupPatient(ctx context.Context, tokenData *domain.TokenData, lastName string, firstName string) ([]domain.Patient, error) {
	name := lastName
	if firstName != "" {
		name = lastName + "," + firstName
	}

	reqBody := AMDLookupRequest{
		PPMDMsg: AMDLookupMsg{
			Action: "lookuppatient",
			Class:  "api",
			Name:   name,
		},
	}

	return c.doPatientLookup(ctx, tokenData, reqBody)
}

// LookupPatientByPhone searches for patients by phone number.
// Phone should be digits only (e.g., "7863344429").
func (c *AdvancedMDClient) LookupPatientByPhone(ctx context.Context, tokenData *domain.TokenData, phone string) ([]domain.Patient, error) {
	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action": "lookuppatient",
			"@class":  "api",
			"@phone":  phone,
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return nil, err
	}

	return parseLookupResponse(body)
}

// doPatientLookup executes a lookuppatient request and parses the response.
func (c *AdvancedMDClient) doPatientLookup(ctx context.Context, tokenData *domain.TokenData, payload interface{}) ([]domain.Patient, error) {
	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return nil, err
	}

	return parseLookupResponse(body)
}

// parseLookupResponse handles AMD's single-vs-array patient response format.
func parseLookupResponse(body []byte) ([]domain.Patient, error) {
	var envelope struct {
		PPMDResults struct {
			Results struct {
				PatientList struct {
					ItemCount string `json:"@itemcount"`
				} `json:"patientlist"`
			} `json:"Results"`
			Error interface{} `json:"Error"`
		} `json:"PPMDResults"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse lookup response: %w", err)
	}
	if err := checkXMLRPCError(body, "lookuppatient"); err != nil {
		return nil, err
	}
	if envelope.PPMDResults.Results.PatientList.ItemCount == "0" {
		return []domain.Patient{}, nil
	}

	// Try array response first
	var arrayResp AMDLookupResponse
	if err := json.Unmarshal(body, &arrayResp); err == nil {
		if arrayResp.PPMDResults.Results.PatientList.Patients != nil {
			return convertPatients(arrayResp.PPMDResults.Results.PatientList.Patients), nil
		}
	}

	// Try single patient response
	var singleResp AMDLookupResponseSingle
	if err := json.Unmarshal(body, &singleResp); err == nil {
		if singleResp.PPMDResults.Results.PatientList.Patient.ID != "" {
			return convertPatients([]AMDPatient{singleResp.PPMDResults.Results.PatientList.Patient}), nil
		}
	}

	return nil, fmt.Errorf("lookuppatient returned malformed patientlist")
}

// AddPatientParams holds the parameters for creating a new patient.
type AddPatientParams struct {
	FirstName string
	LastName  string
	DOB       string
	Phone     string
	Email     string
	Street    string
	AptSuite  string
	City      string
	State     string
	Zip       string
	Sex       string
	SSN       string
	ProfileID string // Provider profile ID for the office (e.g., "620")
}

// AddPatient creates a new patient in AdvancedMD.
// Returns the raw patient ID (with "pat" prefix), responsible party ID, and patient name.
func (c *AdvancedMDClient) AddPatient(ctx context.Context, tokenData *domain.TokenData, params AddPatientParams) (string, string, string, error) {
	name := params.LastName + "," + params.FirstName
	msgTime := time.Now().Format("01/02/2006 03:04:05 PM")

	profileID := params.ProfileID
	if profileID == "" {
		log.Printf("WARNING: addpatient called without ProfileID, defaulting to 620")
		profileID = "620"
	}

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":   "addpatient",
			"@class":    "api",
			"@msgtime":  msgTime,
			"@nocookie": "0",
			"patientlist": map[string]interface{}{
				"patient": map[string]interface{}{
					"@respparty":         "SELF",
					"@name":              name,
					"@sex":               params.Sex,
					"@relationship":      "1",
					"@hipaarelationship": "18",
					"@dob":               params.DOB,
					"@ssn":               strings.TrimSpace(params.SSN),
					"@chart":             "AUTO",
					"@profile":           profileID,
					"address": map[string]interface{}{
						"@address1": params.AptSuite,
						"@address2": params.Street,
						"@city":     params.City,
						"@state":    params.State,
						"@zip":      params.Zip,
					},
					"contactinfo": map[string]interface{}{
						"@homephone": params.Phone,
						"@email":     params.Email,
					},
				},
			},
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return "", "", "", fmt.Errorf("addpatient request failed: %w", err)
	}

	if rejectedByProvider(body) {
		return "", "", "", &ProviderRejectionError{operation: "addpatient"}
	}

	// Try single patient response first (most likely for addpatient)
	var singleResp AMDLookupResponseSingle
	if err := json.Unmarshal(body, &singleResp); err == nil {
		if singleResp.PPMDResults.Results.PatientList.Patient.ID != "" {
			p := singleResp.PPMDResults.Results.PatientList.Patient
			return p.ID, p.RespParty, p.Name, nil
		}
	}

	// Try array response
	var arrayResp AMDLookupResponse
	if err := json.Unmarshal(body, &arrayResp); err == nil {
		if len(arrayResp.PPMDResults.Results.PatientList.Patients) > 0 {
			p := arrayResp.PPMDResults.Results.PatientList.Patients[0]
			return p.ID, p.RespParty, p.Name, nil
		}
	}

	return "", "", "", fmt.Errorf("addpatient returned unexpected response: %s", string(body))
}

// AddInsurance attaches an insurance record to an existing patient in AdvancedMD.
func (c *AdvancedMDClient) AddInsurance(ctx context.Context, tokenData *domain.TokenData, patientID, respPartyID, carrierID, subscriberNum string) error {
	msgTime := time.Now().Format("01/02/2006 03:04:05 PM")

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":  "addinsurance",
			"@class":   "api",
			"@msgtime": msgTime,
			"patient": map[string]interface{}{
				"@id":      patientID,
				"@changed": "1",
				"insplanlist": map[string]interface{}{
					"insplan": map[string]interface{}{
						"@id":                "",
						"@carrier":           carrierID,
						"@subscriber":        respPartyID,
						"@subscribernum":     subscriberNum,
						"@hipaarelationship": "18",
						"@relationship":      "1",
						"@copay":             "0.00",
						"@coverage":          "1",
					},
				},
			},
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return fmt.Errorf("addinsurance request failed: %w", err)
	}

	if err := checkXMLRPCMutationResponse(body, "addinsurance"); err != nil {
		return err
	}

	return nil
}

// EndDateInsurance terminates an existing insurance plan by setting its end date to today.
// Uses the addinsurance action with the existing insplan ID — only @id and @enddate are needed.
func (c *AdvancedMDClient) EndDateInsurance(ctx context.Context, tokenData *domain.TokenData, patientID, insPlanID string) error {
	msgTime := time.Now().Format("01/02/2006 03:04:05 PM")
	today := time.Now().Format("01/02/2006")

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":  "addinsurance",
			"@class":   "api",
			"@msgtime": msgTime,
			"patient": map[string]interface{}{
				"@id":      patientID,
				"@changed": "1",
				"insplanlist": map[string]interface{}{
					"insplan": map[string]interface{}{
						"@id":      insPlanID,
						"@enddate": today,
					},
				},
			},
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return fmt.Errorf("enddate insurance request failed: %w", err)
	}

	if err := checkXMLRPCMutationResponse(body, "enddate insurance"); err != nil {
		return err
	}

	return nil
}

type xmlRPCEnvelope struct {
	PPMDResults *struct {
		Results json.RawMessage `json:"Results"`
		Error   interface{}     `json:"Error"`
	} `json:"PPMDResults"`
}

func parseXMLRPCEnvelope(body []byte, operation string) (*xmlRPCEnvelope, error) {
	var response xmlRPCEnvelope
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%s returned malformed response: %w", operation, err)
	}
	if response.PPMDResults == nil {
		return nil, fmt.Errorf("%s returned unexpected response", operation)
	}
	return &response, nil
}

func checkXMLRPCMutationResponse(body []byte, operation string) error {
	response, err := parseXMLRPCEnvelope(body, operation)
	if err != nil {
		return err
	}
	if providerErrorPresent(response.PPMDResults.Error) {
		return &ProviderRejectionError{operation: operation}
	}
	if len(response.PPMDResults.Results) == 0 || string(response.PPMDResults.Results) == "null" {
		return fmt.Errorf("%s returned unexpected response", operation)
	}
	var results map[string]interface{}
	if err := json.Unmarshal(response.PPMDResults.Results, &results); err != nil || results == nil {
		return fmt.Errorf("%s returned unexpected response", operation)
	}
	return nil
}

func rejectedByProvider(body []byte) bool {
	response, err := parseXMLRPCEnvelope(body, "mutation")
	return err == nil && providerErrorPresent(response.PPMDResults.Error)
}

func providerErrorPresent(value interface{}) bool {
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case map[string]interface{}:
		for _, nested := range value {
			if providerErrorPresent(nested) {
				return true
			}
		}
		return false
	case []interface{}:
		for _, nested := range value {
			if providerErrorPresent(nested) {
				return true
			}
		}
		return false
	case bool:
		return value
	case float64:
		return value != 0
	default:
		return false
	}
}

// checkXMLRPCError parses AMD XMLRPC response body for errors.
// AMD returns errors as either a plain string or a nested Fault structure.
func checkXMLRPCError(body []byte, operation string) error {
	response, err := parseXMLRPCEnvelope(body, operation)
	if err != nil {
		return err
	}
	if !providerErrorPresent(response.PPMDResults.Error) {
		return nil
	}
	return &ProviderRejectionError{operation: operation}
}

// DemographicResult holds parsed insurance info from getdemographic.
type DemographicResult struct {
	Name                string
	CarrierName         string // "AETNA"
	CarrierID           string // "car40887"
	InsPlanID           string // "ins8719894" — active insplan ID for end-dating
	RespPartyID         string // "resp21543970" — for new plan's @subscriber
	SubscriberNum       string
	DOB                 string // "01/15/1980"
	InsuranceStateKnown bool
}

// AMDDemographicResults represents the authoritative getdemographic result.
type AMDDemographicResults struct {
	PatientList struct {
		Patient struct {
			ID          string          `json:"@id"`
			Name        string          `json:"@name"`
			RespParty   string          `json:"@respparty"`
			DOB         string          `json:"@dob"`
			InsPlanList json.RawMessage `json:"insplanlist"`
		} `json:"patient"`
	} `json:"patientlist"`
	CarrierList json.RawMessage `json:"carrierlist"`
}

// AMDInsPlanList wraps insurance plans from the demographic response.
type AMDInsPlanList struct {
	InsPlan json.RawMessage `json:"insplan"`
}

// AMDInsPlan represents an insurance plan entry.
type AMDInsPlan struct {
	ID            string `json:"@id"`
	Carrier       string `json:"@carrier"`
	Subscriber    string `json:"@subscriber"`
	SubscriberNum string `json:"@subscribernum"`
	EndDate       string `json:"@enddate"`
	Coverage      string `json:"@coverage"`
}

// AMDCarrierList wraps carriers from the demographic response.
type AMDCarrierList struct {
	Carrier json.RawMessage `json:"carrier"`
}

// AMDCarrier represents a carrier entry with its name.
type AMDCarrier struct {
	ID   string `json:"@id"`
	Name string `json:"@name"`
}

// GetDemographic fetches patient demographic info including insurance.
// Returns a DemographicResult with carrier info, active insplan ID, and resp party ID.
func (c *AdvancedMDClient) GetDemographic(ctx context.Context, tokenData *domain.TokenData, patientID string) (*DemographicResult, error) {
	msgTime := time.Now().Format("01/02/2006 03:04:05 PM")

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":    "getdemographic",
			"@class":     "demographics",
			"@msgtime":   msgTime,
			"@patientid": patientID,
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return nil, fmt.Errorf("getdemographic request failed: %w", err)
	}

	envelope, err := parseXMLRPCEnvelope(body, "getdemographic")
	if err != nil {
		return nil, err
	}
	if providerErrorPresent(envelope.PPMDResults.Error) {
		return nil, &ProviderRejectionError{operation: "getdemographic"}
	}
	if len(envelope.PPMDResults.Results) == 0 || string(envelope.PPMDResults.Results) == "null" {
		return nil, fmt.Errorf("getdemographic returned unexpected response")
	}

	var results AMDDemographicResults
	if err := json.Unmarshal(envelope.PPMDResults.Results, &results); err != nil {
		return nil, fmt.Errorf("failed to parse demographic response: %w", err)
	}
	patient := results.PatientList.Patient
	if patient.ID == "" {
		return nil, fmt.Errorf("getdemographic returned unexpected response")
	}
	if domain.StripPatientPrefix(patient.ID) != domain.StripPatientPrefix(patientID) {
		return nil, fmt.Errorf("getdemographic returned mismatched patient")
	}

	result := &DemographicResult{
		Name:                patient.Name,
		RespPartyID:         patient.RespParty,
		DOB:                 patient.DOB,
		InsuranceStateKnown: true,
	}

	// Parse insplanlist to get insurance details
	if patient.InsPlanList == nil {
		return result, nil
	}

	var planList AMDInsPlanList
	if err := json.Unmarshal(patient.InsPlanList, &planList); err != nil {
		result.InsuranceStateKnown = false
		return result, nil
	}
	if planList.InsPlan == nil {
		return result, nil
	}

	// Find the active primary plan (enddate empty, coverage "1")
	// Handle both single object and array responses from AMD
	var activePlan *AMDInsPlan
	var single AMDInsPlan
	if err := json.Unmarshal(planList.InsPlan, &single); err == nil && single.Carrier != "" {
		if isActivePrimaryPlan(single) {
			activePlan = &single
		}
	} else {
		var plans []AMDInsPlan
		if err := json.Unmarshal(planList.InsPlan, &plans); err != nil {
			result.InsuranceStateKnown = false
			return result, nil
		}
		for i := range plans {
			if isActivePrimaryPlan(plans[i]) {
				if activePlan != nil {
					result.InsuranceStateKnown = false
					return result, nil
				}
				activePlan = &plans[i]
			}
		}
	}

	if activePlan == nil {
		return result, nil
	}

	result.CarrierID = activePlan.Carrier
	result.InsPlanID = activePlan.ID
	result.SubscriberNum = activePlan.SubscriberNum
	if activePlan.Subscriber != "" {
		result.RespPartyID = activePlan.Subscriber
	}

	// Look up carrier name from carrierlist
	if results.CarrierList == nil {
		result.CarrierName = result.CarrierID
		return result, nil
	}

	var carrierList AMDCarrierList
	if err := json.Unmarshal(results.CarrierList, &carrierList); err != nil {
		result.CarrierName = result.CarrierID
		return result, nil
	}

	// Try single carrier
	var singleCarrier AMDCarrier
	if err := json.Unmarshal(carrierList.Carrier, &singleCarrier); err == nil {
		if singleCarrier.ID == result.CarrierID {
			result.CarrierName = singleCarrier.Name
			return result, nil
		}
	}

	// Try array of carriers
	var carriers []AMDCarrier
	if err := json.Unmarshal(carrierList.Carrier, &carriers); err == nil {
		for _, c := range carriers {
			if c.ID == result.CarrierID {
				result.CarrierName = c.Name
				return result, nil
			}
		}
	}

	result.CarrierName = result.CarrierID
	return result, nil
}

func isActivePrimaryPlan(plan AMDInsPlan) bool {
	return plan.EndDate == "" && (plan.Coverage == "" || plan.Coverage == "1")
}

// convertPatients converts AMD patient records to domain patients.
func convertPatients(amdPatients []AMDPatient) []domain.Patient {
	patients := make([]domain.Patient, len(amdPatients))
	for i, p := range amdPatients {
		patients[i] = domain.Patient{
			ID:        domain.StripPatientPrefix(p.ID),
			FullName:  p.Name,
			FirstName: domain.ParseFirstName(p.Name),
			DOB:       p.DOB,
			Phone:     bestPatientPhone(p.ContactInfo),
		}
	}
	return patients
}

func bestPatientPhone(contact AMDContactInfo) string {
	for _, phone := range []string{
		contact.CellPhone,
		contact.MobilePhone,
		contact.HomePhone,
		contact.WorkPhone,
		contact.OfficePhone,
		contact.OtherPhone,
	} {
		if strings.TrimSpace(phone) != "" {
			return phone
		}
	}
	return ""
}

// AMDSchedulerSetupResponse represents the getschedulersetup response structure.
type AMDSchedulerSetupResponse struct {
	PPMDResults struct {
		Results struct {
			ColumnList   AMDColumnList   `json:"columnlist"`
			ProfileList  AMDProfileList  `json:"profilelist"`
			FacilityList AMDFacilityList `json:"facilitylist"`
		} `json:"Results"`
		Error interface{} `json:"Error"`
	} `json:"PPMDResults"`
}

// AMDColumnList holds the list of scheduler columns.
type AMDColumnList struct {
	Columns interface{} `json:"column"` // Can be single object or array
}

// AMDProfileList holds the list of provider profiles.
type AMDProfileList struct {
	Profiles interface{} `json:"profile"` // Can be single object or array
}

// AMDFacilityList holds the list of facilities.
type AMDFacilityList struct {
	Facilities interface{} `json:"facility"` // Can be single object or array
}

// GetSchedulerSetup retrieves the scheduler configuration from AdvancedMD.
func (c *AdvancedMDClient) GetSchedulerSetup(ctx context.Context, tokenData *domain.TokenData) (*domain.SchedulerSetup, error) {
	msgTime := time.Now().Format("01/02/2006 03:04:05 PM")

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":   "getschedulersetup",
			"@class":    "masterfiles",
			"@msgtime":  msgTime,
			"@nocookie": "0",
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return nil, fmt.Errorf("getschedulersetup request failed: %w", err)
	}

	var resp AMDSchedulerSetupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse scheduler setup response: %w", err)
	}

	setup := &domain.SchedulerSetup{
		Columns:    parseColumns(resp.PPMDResults.Results.ColumnList.Columns),
		Profiles:   parseProfiles(resp.PPMDResults.Results.ProfileList.Profiles),
		Facilities: parseFacilities(resp.PPMDResults.Results.FacilityList.Facilities),
	}

	return setup, nil
}

// parseColumns converts the AMD column data to domain columns.
func parseColumns(data interface{}) []domain.SchedulerColumn {
	if data == nil {
		return nil
	}

	var columns []domain.SchedulerColumn

	switch v := data.(type) {
	case map[string]interface{}:
		// Single column
		columns = append(columns, parseColumnFromMap(v))
	case []interface{}:
		// Array of columns
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				columns = append(columns, parseColumnFromMap(m))
			}
		}
	}

	return columns
}

// parseColumnFromMap extracts a SchedulerColumn from a map.
func parseColumnFromMap(m map[string]interface{}) domain.SchedulerColumn {
	col := domain.SchedulerColumn{
		ID:         stripPrefix(getString(m, "@id"), "col"),
		Name:       getString(m, "@name"),
		ProfileID:  stripPrefix(getString(m, "@profile"), "prof"),
		FacilityID: stripPrefix(getString(m, "@facility"), "fac"),
	}

	// Get settings from nested columnsetting object
	if settings, ok := m["columnsetting"].(map[string]interface{}); ok {
		col.StartTime = normalizeTime(getString(settings, "@start"))
		col.EndTime = normalizeTime(getString(settings, "@end"))
		col.Interval = getInt(settings, "@interval")
		col.MaxApptsPerSlot = getInt(settings, "@maxapptsperslot")
		col.Workweek = parseWorkweek(getString(settings, "@workweek"))
	}

	return col
}

// stripPrefix removes a prefix from a string (e.g., "col1716" -> "1716").
func stripPrefix(s, prefix string) string {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

// parseWorkweek converts AMD workweek string "1111100" to bitmask.
// AMD format: 7 chars for Mon-Sun (1=works, 0=off)
// Our format: bitmask with 1=Sun, 2=Mon, 4=Tue, etc.
func parseWorkweek(ww string) int {
	if len(ww) != 7 {
		return 0
	}
	// AMD: index 0=Mon, 1=Tue, ..., 6=Sun
	// Our: bit 0=Sun, 1=Mon, 2=Tue, ..., 6=Sat
	bitmask := 0
	amdToBit := []int{1, 2, 3, 4, 5, 6, 0} // Mon->1, Tue->2, ..., Sun->0
	for i, ch := range ww {
		if ch == '1' {
			bitmask |= (1 << amdToBit[i])
		}
	}
	return bitmask
}

// parseProfiles converts the AMD profile data to domain profiles.
func parseProfiles(data interface{}) []domain.SchedulerProfile {
	if data == nil {
		return nil
	}

	var profiles []domain.SchedulerProfile

	switch v := data.(type) {
	case map[string]interface{}:
		profiles = append(profiles, domain.SchedulerProfile{
			ID:   stripPrefix(getString(v, "@id"), "prof"),
			Code: getString(v, "@code"),
			Name: getString(v, "@name"),
		})
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				profiles = append(profiles, domain.SchedulerProfile{
					ID:   stripPrefix(getString(m, "@id"), "prof"),
					Code: getString(m, "@code"),
					Name: getString(m, "@name"),
				})
			}
		}
	}

	return profiles
}

// parseFacilities converts the AMD facility data to domain facilities.
func parseFacilities(data interface{}) []domain.SchedulerFacility {
	if data == nil {
		return nil
	}

	var facilities []domain.SchedulerFacility

	switch v := data.(type) {
	case map[string]interface{}:
		facilities = append(facilities, domain.SchedulerFacility{
			ID:   stripPrefix(getString(v, "@id"), "fac"),
			Code: getString(v, "@code"),
			Name: getString(v, "@name"),
		})
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				facilities = append(facilities, domain.SchedulerFacility{
					ID:   stripPrefix(getString(m, "@id"), "fac"),
					Code: getString(m, "@code"),
					Name: getString(m, "@name"),
				})
			}
		}
	}

	return facilities
}

// getString safely extracts a string from a map.
func getString(m map[string]interface{}, key string) string {
	s, _ := m[key].(string)
	return s
}

// getInt safely extracts an int from a map (handles string or number).
func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case string:
			var i int
			fmt.Sscanf(n, "%d", &i)
			return i
		}
	}
	return 0
}

// normalizeTime converts AMD time formats (e.g., "0800", "08:00", "8:00 AM") to "HH:MM".
func normalizeTime(t string) string {
	if t == "" {
		return ""
	}

	// Already in HH:MM format
	if len(t) == 5 && t[2] == ':' {
		return t
	}

	// Handle "H:MM" format (e.g., "8:00") — must check before HHMM
	if len(t) == 4 && t[1] == ':' {
		return "0" + t
	}

	// Handle "HHMM" format (e.g., "0800")
	if len(t) == 4 {
		return t[:2] + ":" + t[2:]
	}

	return t
}
