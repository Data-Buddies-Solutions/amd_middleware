package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/session"
)

type AMDLookupRequest struct {
	PPMDMsg AMDLookupMsg `json:"ppmdmsg"`
}

type AMDLookupMsg struct {
	Action string `json:"@action"`
	Class  string `json:"@class"`
	Name   string `json:"@name,omitempty"`
	Phone  string `json:"@phone,omitempty"`
	DOB    string `json:"@dob,omitempty"`
	Page   int    `json:"@page,omitempty"`
}

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

type AdvancedMDClient struct {
	httpClient *http.Client
}

type ProviderRejectionError struct {
	operation string
	code      string
}

func (e *ProviderRejectionError) Error() string {
	return e.operation + " rejected by provider"
}

type HTTPStatusError struct {
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected status %d", e.StatusCode)
}

func NewAdvancedMDClient(httpClient *http.Client) *AdvancedMDClient {
	return &AdvancedMDClient{httpClient: httpClient}
}

func (c *AdvancedMDClient) doXMLRPCRequest(ctx context.Context, tokenData *session.TokenData, payload interface{}) ([]byte, error) {
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
	observeProviderStatus(ctx, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode}
	}

	return body, nil
}

func (c *AdvancedMDClient) LookupPatient(ctx context.Context, tokenData *session.TokenData, lastName string, firstName string) ([]domain.Patient, error) {
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

	read, err := c.doPatientLookup(ctx, tokenData, reqBody)
	return read.Patients, err
}

func (c *AdvancedMDClient) LookupPatientCandidates(ctx context.Context, tokenData *session.TokenData, firstName, dob string) (read domain.PatientCandidateRead, resultErr error) {
	read, err := c.doPatientLookup(ctx, tokenData, AMDLookupRequest{PPMDMsg: AMDLookupMsg{Action: "lookuppatient", Class: "api", Name: "," + firstName, DOB: dob}})
	if err != nil {
		return read, err
	}
	for index, patient := range read.Patients {
		parts := strings.SplitN(patient.FullName, ",", 2)
		if len(parts) == 2 {
			patient.LastName = strings.TrimSpace(parts[0])
			if names := strings.Fields(parts[1]); len(names) > 0 {
				patient.FirstName = names[0]
			}
			read.Patients[index].LastName = patient.LastName
			read.Patients[index].FirstName = patient.FirstName
		}
	}
	return read, nil
}

func (c *AdvancedMDClient) LookupPatientByPhone(ctx context.Context, tokenData *session.TokenData, phone string) ([]domain.Patient, error) {
	payload := AMDLookupRequest{PPMDMsg: AMDLookupMsg{
		Action: "lookuppatient", Class: "api", Phone: phone,
	}}

	read, err := c.doPatientLookup(ctx, tokenData, payload)
	return read.Patients, err
}

func (c *AdvancedMDClient) doPatientLookup(ctx context.Context, tokenData *session.TokenData, payload AMDLookupRequest) (read domain.PatientCandidateRead, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "lookuppatient")
	defer func() { finish(resultErr) }()
	const maxLookupPages = 100
	patients := []domain.Patient{}
	seen := map[string]bool{}
	expectedPages, expectedTotal := 0, 0
	for page := 1; page <= maxLookupPages; page++ {
		payload.PPMDMsg.Page = page
		body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
		if err != nil {
			return domain.PatientCandidateRead{}, err
		}
		pageRead, meta, err := parseLookupResponse(body)
		if err != nil {
			return domain.PatientCandidateRead{}, err
		}
		batch := pageRead.Patients
		for _, patient := range batch {
			if patient.ID == "" || seen[patient.ID] {
				return domain.PatientCandidateRead{}, fmt.Errorf("patient lookup returned missing or repeated patient ID")
			}
			seen[patient.ID] = true
			patients = append(patients, patient)
		}
		if page == 1 && meta.Page == "" && meta.PageCount == "" {
			if meta.ItemCount != "" {
				total, err := strconv.Atoi(meta.ItemCount)
				if err != nil || total != len(patients) {
					return domain.PatientCandidateRead{}, fmt.Errorf("incomplete legacy patient lookup results")
				}
			}
			return domain.PatientCandidateRead{Patients: patients, Complete: false}, nil
		}
		currentPage, pageErr := strconv.Atoi(meta.Page)
		pageCount, countErr := strconv.Atoi(meta.PageCount)
		total, totalErr := strconv.Atoi(meta.ItemCount)
		if pageErr != nil || countErr != nil || totalErr != nil || currentPage != page || pageCount < 0 || pageCount > maxLookupPages || total < 0 {
			return domain.PatientCandidateRead{}, fmt.Errorf("invalid or excessive patient lookup pagination")
		}
		if page == 1 && total == 0 && len(batch) == 0 && pageCount <= 1 {
			if !pageRead.Complete {
				return domain.PatientCandidateRead{}, fmt.Errorf("contradictory empty patient lookup")
			}
			return domain.PatientCandidateRead{Patients: patients, Complete: true}, nil
		}
		if pageCount < page || len(batch) == 0 {
			return domain.PatientCandidateRead{}, fmt.Errorf("incomplete patient lookup page")
		}
		if page == 1 {
			expectedPages, expectedTotal = pageCount, total
		}
		if pageCount != expectedPages || total != expectedTotal {
			return domain.PatientCandidateRead{}, fmt.Errorf("patient lookup changed during pagination")
		}
		if page == pageCount {
			if len(patients) < total {
				return domain.PatientCandidateRead{}, fmt.Errorf("incomplete patient lookup results")
			}
			return domain.PatientCandidateRead{Patients: patients, Complete: len(patients) == total}, nil
		}
	}
	return domain.PatientCandidateRead{}, fmt.Errorf("patient lookup exceeded page limit")
}

type patientList struct {
	ItemCount string          `json:"@itemcount"`
	Page      string          `json:"@page"`
	PageCount string          `json:"@pagecount"`
	Patients  json.RawMessage `json:"patient"`
}

func parseLookupResponse(body []byte) (domain.PatientCandidateRead, patientList, error) {
	results, err := decodeXMLRPC(body, "lookuppatient")
	if err != nil {
		return domain.PatientCandidateRead{}, patientList{}, err
	}
	var parsed struct {
		PatientList *patientList `json:"patientlist"`
	}
	if json.Unmarshal(results, &parsed) != nil || parsed.PatientList == nil {
		return domain.PatientCandidateRead{}, patientList{}, fmt.Errorf("lookuppatient returned malformed patientlist")
	}
	list := *parsed.PatientList
	patients, patientsErr := oneOrMany[AMDPatient](list.Patients)
	pages, pageErr := strconv.Atoi(list.PageCount)
	if list.ItemCount == "0" {
		empty := patientsErr == nil && len(patients) == 0
		return domain.PatientCandidateRead{
			Patients: []domain.Patient{},
			Complete: empty && list.Page == "1" && pageErr == nil && (pages == 0 || pages == 1),
		}, list, nil
	}
	if patientsErr != nil || len(patients) == 0 {
		return domain.PatientCandidateRead{}, patientList{}, fmt.Errorf("lookuppatient returned malformed patientlist")
	}
	count, countErr := strconv.Atoi(list.ItemCount)
	return domain.PatientCandidateRead{
		Patients: convertPatients(patients),
		Complete: countErr == nil && pageErr == nil && list.Page == "1" && pages == 1 && count == len(patients),
	}, list, nil
}

func (c *AdvancedMDClient) AddPatient(ctx context.Context, tokenData *session.TokenData, patient domain.PatientCreate, profileID string) (created domain.CreatedPatient, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "addpatient")
	defer func() { finish(resultErr) }()

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":   "addpatient",
			"@class":    "api",
			"@msgtime":  msgTime(),
			"@nocookie": "0",
			"patientlist": map[string]interface{}{
				"patient": map[string]interface{}{
					"@respparty":         "SELF",
					"@name":              patient.LastName + "," + patient.FirstName,
					"@sex":               patient.Sex,
					"@relationship":      "1",
					"@hipaarelationship": "18",
					"@dob":               patient.DOB,
					"@ssn":               strings.TrimSpace(patient.SSN),
					"@chart":             "AUTO",
					"@profile":           profileID,
					"address": map[string]interface{}{
						"@address1": patient.AptSuite,
						"@address2": patient.Street,
						"@city":     patient.City,
						"@state":    patient.State,
						"@zip":      patient.Zip,
					},
					"contactinfo": map[string]interface{}{
						"@homephone": patient.Phone,
						"@email":     patient.Email,
					},
				},
			},
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return domain.CreatedPatient{}, fmt.Errorf("addpatient request failed: %w", err)
	}
	results, err := decodeXMLRPC(body, "addpatient")
	if err != nil {
		return domain.CreatedPatient{}, err
	}
	var parsed struct {
		PatientList struct {
			Patient json.RawMessage `json:"patient"`
		} `json:"patientlist"`
	}
	if json.Unmarshal(results, &parsed) == nil {
		patients, err := oneOrMany[AMDPatient](parsed.PatientList.Patient)
		if err == nil && len(patients) > 0 && patients[0].ID != "" {
			return domain.CreatedPatient{
				ID:          domain.StripPatientPrefix(patients[0].ID),
				RespPartyID: patients[0].RespParty,
				Name:        patients[0].Name,
			}, nil
		}
	}
	return domain.CreatedPatient{}, fmt.Errorf("addpatient returned unexpected response")
}

func (c *AdvancedMDClient) AddInsurance(ctx context.Context, tokenData *session.TokenData, patientID, respPartyID, carrierID, subscriberNum string) (resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "addinsurance")
	defer func() { finish(resultErr) }()
	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":  "addinsurance",
			"@class":   "api",
			"@msgtime": msgTime(),
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

	if err := checkXMLRPCMutation(body, "addinsurance"); err != nil {
		return err
	}

	return nil
}

func (c *AdvancedMDClient) EndDateInsurance(ctx context.Context, tokenData *session.TokenData, patientID, insPlanID string) (resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "enddateinsurance")
	defer func() { finish(resultErr) }()
	today := time.Now().Format("01/02/2006")

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":  "addinsurance",
			"@class":   "api",
			"@msgtime": msgTime(),
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

	if err := checkXMLRPCMutation(body, "enddate insurance"); err != nil {
		return err
	}

	return nil
}

func msgTime() string {
	return time.Now().Format("01/02/2006 03:04:05 PM")
}

func decodeXMLRPC(body []byte, operation string) (json.RawMessage, error) {
	var envelope struct {
		PPMDResults *struct {
			Results json.RawMessage `json:"Results"`
			Error   interface{}     `json:"Error"`
		} `json:"PPMDResults"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s returned malformed response: %w", operation, err)
	}
	if envelope.PPMDResults == nil {
		return nil, fmt.Errorf("%s returned unexpected response", operation)
	}
	if providerErrorPresent(envelope.PPMDResults.Error) {
		return nil, providerRejection(operation, body)
	}
	return envelope.PPMDResults.Results, nil
}

func oneOrMany[T any](raw json.RawMessage) ([]T, error) {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0 || string(raw) == "null":
		return nil, nil
	case raw[0] == '[':
		var items []T
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		return items, nil
	case raw[0] == '{':
		var item T
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		return []T{item}, nil
	default:
		return nil, fmt.Errorf("expected an object or array")
	}
}

func checkXMLRPCMutation(body []byte, operation string) error {
	results, err := decodeXMLRPC(body, operation)
	if err != nil {
		return err
	}
	var fields map[string]interface{}
	if json.Unmarshal(results, &fields) != nil || fields == nil {
		return fmt.Errorf("%s returned unexpected response", operation)
	}
	return nil
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

type AMDInsPlan struct {
	ID            string `json:"@id"`
	Carrier       string `json:"@carrier"`
	Subscriber    string `json:"@subscriber"`
	SubscriberNum string `json:"@subscribernum"`
	EndDate       string `json:"@enddate"`
	Coverage      string `json:"@coverage"`
}

type AMDCarrier struct {
	ID   string `json:"@id"`
	Name string `json:"@name"`
}

func (c *AdvancedMDClient) GetDemographic(ctx context.Context, tokenData *session.TokenData, patientID string) (demographics domain.PatientDemographics, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "getdemographic")
	defer func() { finish(resultErr) }()

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":    "getdemographic",
			"@class":     "demographics",
			"@msgtime":   msgTime(),
			"@patientid": patientID,
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return domain.PatientDemographics{}, fmt.Errorf("getdemographic request failed: %w", err)
	}
	results, err := decodeXMLRPC(body, "getdemographic")
	if err != nil {
		return domain.PatientDemographics{}, err
	}
	var parsed AMDDemographicResults
	if err := json.Unmarshal(results, &parsed); err != nil {
		return domain.PatientDemographics{}, fmt.Errorf("failed to parse demographic response: %w", err)
	}
	patient := parsed.PatientList.Patient
	if patient.ID == "" {
		return domain.PatientDemographics{}, fmt.Errorf("getdemographic returned unexpected response")
	}
	if domain.StripPatientPrefix(patient.ID) != domain.StripPatientPrefix(patientID) {
		return domain.PatientDemographics{}, fmt.Errorf("getdemographic returned mismatched patient")
	}

	demographics = domain.PatientDemographics{
		FullName:            patient.Name,
		RespPartyID:         patient.RespParty,
		DOB:                 patient.DOB,
		InsuranceStateKnown: true,
	}
	activePlan, known := activePrimaryPlan(patient.InsPlanList)
	if !known {
		demographics.InsuranceStateKnown = false
		return demographics, nil
	}
	if activePlan == nil {
		return demographics, nil
	}
	demographics.CarrierID = activePlan.Carrier
	demographics.CarrierName = carrierName(parsed.CarrierList, activePlan.Carrier)
	demographics.InsPlanID = activePlan.ID
	demographics.SubscriberNum = activePlan.SubscriberNum
	if activePlan.Subscriber != "" {
		demographics.RespPartyID = activePlan.Subscriber
	}
	return demographics, nil
}

func activePrimaryPlan(insPlanList json.RawMessage) (*AMDInsPlan, bool) {
	var list struct {
		InsPlan json.RawMessage `json:"insplan"`
	}
	if insPlanList != nil && json.Unmarshal(insPlanList, &list) != nil {
		return nil, false
	}
	plans, err := oneOrMany[AMDInsPlan](list.InsPlan)
	if err != nil {
		return nil, false
	}
	var active *AMDInsPlan
	for i := range plans {
		if !isActivePrimaryPlan(plans[i]) {
			continue
		}
		if active != nil || plans[i].Carrier == "" {
			return nil, false
		}
		active = &plans[i]
	}
	return active, true
}

func carrierName(carrierList json.RawMessage, carrierID string) string {
	var list struct {
		Carrier json.RawMessage `json:"carrier"`
	}
	if carrierList == nil || json.Unmarshal(carrierList, &list) != nil {
		return carrierID
	}
	carriers, _ := oneOrMany[AMDCarrier](list.Carrier)
	for _, carrier := range carriers {
		if carrier.ID == carrierID {
			return carrier.Name
		}
	}
	return carrierID
}

func isActivePrimaryPlan(plan AMDInsPlan) bool {
	return plan.EndDate == "" && (plan.Coverage == "" || plan.Coverage == "1")
}

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

type amdAttribute string

func (a *amdAttribute) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		*a = amdAttribute(text)
		return nil
	}
	var number json.Number
	if json.Unmarshal(data, &number) == nil {
		*a = amdAttribute(number)
	}
	return nil
}

func (a amdAttribute) int() int {
	var n int
	fmt.Sscanf(string(a), "%d", &n)
	return n
}

type amdColumn struct {
	ID       amdAttribute    `json:"@id"`
	Name     amdAttribute    `json:"@name"`
	Profile  amdAttribute    `json:"@profile"`
	Facility amdAttribute    `json:"@facility"`
	Setting  json.RawMessage `json:"columnsetting"`
}

type amdColumnSetting struct {
	Start           amdAttribute `json:"@start"`
	End             amdAttribute `json:"@end"`
	Interval        amdAttribute `json:"@interval"`
	MaxApptsPerSlot amdAttribute `json:"@maxapptsperslot"`
	Workweek        amdAttribute `json:"@workweek"`
}

type amdSetupItem struct {
	ID   amdAttribute `json:"@id"`
	Code amdAttribute `json:"@code"`
	Name amdAttribute `json:"@name"`
}

func (c *AdvancedMDClient) GetSchedulerSetup(ctx context.Context, tokenData *session.TokenData) (setupResult *domain.SchedulerSetup, resultErr error) {
	ctx, finish := beginProviderOperation(ctx, "getschedulersetup")
	defer func() { finish(resultErr) }()

	payload := map[string]interface{}{
		"ppmdmsg": map[string]interface{}{
			"@action":   "getschedulersetup",
			"@class":    "masterfiles",
			"@msgtime":  msgTime(),
			"@nocookie": "0",
		},
	}

	body, err := c.doXMLRPCRequest(ctx, tokenData, payload)
	if err != nil {
		return nil, fmt.Errorf("getschedulersetup request failed: %w", err)
	}
	results, err := decodeXMLRPC(body, "getschedulersetup")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		ColumnList *struct {
			Columns json.RawMessage `json:"column"`
		} `json:"columnlist"`
		ProfileList struct {
			Profiles json.RawMessage `json:"profile"`
		} `json:"profilelist"`
		FacilityList struct {
			Facilities json.RawMessage `json:"facility"`
		} `json:"facilitylist"`
	}
	if err := json.Unmarshal(results, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse scheduler setup response: %w", err)
	}
	if parsed.ColumnList == nil || len(bytes.TrimSpace(parsed.ColumnList.Columns)) == 0 || string(bytes.TrimSpace(parsed.ColumnList.Columns)) == "null" {
		return nil, fmt.Errorf("scheduler setup returned unexpected response: missing columnlist")
	}
	columns, err := oneOrMany[amdColumn](parsed.ColumnList.Columns)
	if err != nil {
		return nil, fmt.Errorf("scheduler setup returned invalid column collection")
	}
	profiles, _ := oneOrMany[amdSetupItem](parsed.ProfileList.Profiles)
	facilities, _ := oneOrMany[amdSetupItem](parsed.FacilityList.Facilities)

	setup := &domain.SchedulerSetup{}
	for _, column := range columns {
		setup.Columns = append(setup.Columns, schedulerColumn(column))
	}
	for _, profile := range profiles {
		setup.Profiles = append(setup.Profiles, domain.SchedulerProfile{
			ID: strings.TrimPrefix(string(profile.ID), "prof"), Code: string(profile.Code), Name: string(profile.Name),
		})
	}
	for _, facility := range facilities {
		setup.Facilities = append(setup.Facilities, domain.SchedulerFacility{
			ID: strings.TrimPrefix(string(facility.ID), "fac"), Code: string(facility.Code), Name: string(facility.Name),
		})
	}
	return setup, nil
}

func schedulerColumn(column amdColumn) domain.SchedulerColumn {
	result := domain.SchedulerColumn{
		ID:         strings.TrimPrefix(string(column.ID), "col"),
		Name:       string(column.Name),
		ProfileID:  strings.TrimPrefix(string(column.Profile), "prof"),
		FacilityID: strings.TrimPrefix(string(column.Facility), "fac"),
	}
	var setting amdColumnSetting
	if json.Unmarshal(column.Setting, &setting) == nil {
		result.StartTime = normalizeTime(string(setting.Start))
		result.EndTime = normalizeTime(string(setting.End))
		result.Interval = setting.Interval.int()
		result.MaxApptsPerSlot = setting.MaxApptsPerSlot.int()
		result.Workweek = parseWorkweek(string(setting.Workweek))
	}
	return result
}

func parseWorkweek(ww string) int {
	if len(ww) != 7 {
		return 0
	}
	bitmask := 0
	amdToBit := []int{1, 2, 3, 4, 5, 6, 0}
	for i, ch := range ww {
		if ch == '1' {
			bitmask |= (1 << amdToBit[i])
		}
	}
	return bitmask
}

func normalizeTime(t string) string {
	if t == "" {
		return ""
	}

	if len(t) == 5 && t[2] == ':' {
		return t
	}

	if len(t) == 4 && t[1] == ':' {
		return "0" + t
	}

	if len(t) == 4 {
		return t[:2] + ":" + t[2:]
	}

	return t
}
