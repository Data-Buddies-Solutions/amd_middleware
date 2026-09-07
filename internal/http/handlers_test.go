package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
	patientmodule "advancedmd-token-management/internal/patient"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
	"advancedmd-token-management/internal/session"
)

func TestHandleLive(t *testing.T) {
	handlers := &Handlers{}

	req := httptest.NewRequest("GET", "/live", nil)
	w := httptest.NewRecorder()

	handlers.HandleLive(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", body["status"])
	}
}

func TestMetricsEndpointExposesSafePatientMutationOutcomes(t *testing.T) {
	const patientID = "patient-identifier-must-not-appear"
	patientmodule.New(advancedmdtest.NewAdapter()).UpdateInsurance(context.Background(), patientmodule.UpdateInsuranceCommand{
		PatientID: patientID,
	})

	router := NewRouter(NewHandlers(nil, nil, nil), "test-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `patient_mutation_outcomes_total{operation="update_insurance",outcome="validation_failed"}`) {
		t.Fatalf("metrics body = %q", body)
	}
	if strings.Contains(body, patientID) {
		t.Fatalf("metrics exposed patient identifier: %q", body)
	}
}

func TestPatientResolveKeepsStableResponseWhenSessionUnavailable(t *testing.T) {
	records := advancedmd.NewAdapter(unavailableSession{}, nil, nil)
	handlers := NewHandlers(unavailableSession{}, patientmodule.New(records), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/patient/resolve", strings.NewReader(`{"patientId":"123"}`))
	w := httptest.NewRecorder()

	handlers.HandlePatientResolve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", w.Code)
	}
	var body ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "error" {
		t.Fatalf("status = %q, want error", body.Status)
	}
	if body.Message != "Service authentication is temporarily unavailable. Please try again." {
		t.Fatalf("message = %q", body.Message)
	}
}

func TestHandlePatientResolveMapsPatientModuleResult(t *testing.T) {
	domain.InitRegistry("")
	amd := advancedmdtest.NewAdapter()
	amd.PatientSearches[domain.PatientSearch{Phone: "9542872010"}] = []domain.Patient{{
		ID: "123", FullName: "DOE,JANE", DOB: "01/15/1980", Phone: "850-373-3869",
	}}
	amd.Demographics["123"] = domain.PatientDemographics{
		CarrierName: "HUMANA MEDICARE",
		CarrierID:   "car40906",
	}
	handlers := NewHandlers(nil, patientmodule.New(amd), nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/patient/resolve",
		strings.NewReader(`{"phone":"(954) 287-2010","office":"Spring Hill"}`),
	)
	w := httptest.NewRecorder()
	handlers.HandlePatientResolve(w, req)

	var body PatientResolveResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "verified" || body.PatientID != "123" || body.Phone != "850-373-3869" {
		t.Fatalf("response = %+v", body)
	}
	if body.Routing != "bach_only" || len(body.AllowedProviders) != 1 {
		t.Fatalf("routing response = %+v", body)
	}
	if body.AppointmentsStatus != "none" || body.Appointments == nil {
		t.Fatalf("appointments response = %+v", body)
	}
}

type unavailableSession struct{}

func (unavailableSession) Get(context.Context) (*domain.TokenData, error) {
	return nil, session.ErrSessionUnavailable
}

func (unavailableSession) Maintain(context.Context) error {
	return session.ErrSessionUnavailable
}

func (unavailableSession) Status() session.SessionStatus {
	return session.SessionStatus{State: session.SessionUnavailable}
}

func TestHandleGetAvailability_InvalidDOB(t *testing.T) {
	handlers := &Handlers{scheduling: schedulingStub{err: errors.New("dob must be a valid date")}}
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	body := fmt.Sprintf(`{"requestedDate":%q,"office":"Hollywood","routing":"optical_only","dob":"not-a-date"}`, date)
	req := httptest.NewRequest("POST", "/api/scheduler/availability", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleGetAvailability(w, req)

	var resp ErrorResponse
	json.NewDecoder(w.Result().Body).Decode(&resp)
	if resp.Status != "error" {
		t.Fatalf("expected status error, got %q", resp.Status)
	}
	if resp.Message != "dob must be a valid date" {
		t.Fatalf("expected invalid DOB message, got %q", resp.Message)
	}
}

func TestHandleGetAvailabilityMapsSchedulingResult(t *testing.T) {
	scheduler := schedulingStub{
		result: domain.AvailabilityResponse{
			Status:                domain.AvailabilityStatusSuccess,
			Outcome:               domain.AvailabilityOutcomeFound,
			AvailabilityFound:     true,
			RequestedDate:         "2026-06-03",
			ActualDate:            "2026-06-03",
			SearchedFrom:          "2026-06-03",
			SearchedThrough:       "2026-06-03",
			ShouldRetrySameSearch: false,
			NextAction:            domain.AvailabilityNextActionOfferSlots,
			Slots: []domain.AvailabilitySlotOption{{
				Provider:     "Dr. Austin Bach",
				Time:         "9:00 AM",
				DateTime:     "2026-06-03T09:00",
				BookingToken: "signed-slot",
				ColumnID:     1513,
				ProfileID:    620,
				Duration:     15,
			}},
		},
	}
	handlers := &Handlers{scheduling: scheduler}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/scheduler/availability",
		strings.NewReader(`{"requestedDate":"2026-06-03","office":"Spring Hill","routing":"bach_only","dob":"01/15/1980"}`),
	)
	w := httptest.NewRecorder()

	handlers.HandleGetAvailability(w, req)

	var response domain.AvailabilityResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Outcome != domain.AvailabilityOutcomeFound ||
		len(response.Slots) != 1 ||
		response.Slots[0].BookingToken != "signed-slot" {
		t.Fatalf("response = %#v", response)
	}
}

func TestAvailabilityRouteRetainsAuthenticationAndResponseContract(t *testing.T) {
	handlers := &Handlers{scheduling: schedulingStub{
		result: domain.AvailabilityResponse{
			Status:                domain.AvailabilityStatusSuccess,
			Outcome:               domain.AvailabilityOutcomeNoAvailability,
			AvailabilityFound:     false,
			RequestedDate:         "2026-06-03",
			SearchedFrom:          "2026-06-03",
			SearchedThrough:       "2026-06-17",
			ShouldRetrySameSearch: false,
			NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
			Slots:                 []domain.AvailabilitySlotOption{},
		},
	}}
	router := NewRouter(handlers, "agent-secret", nil)
	body := `{"requestedDate":"2026-06-03","office":"Spring Hill","routing":"bach_only"}`

	unauthenticated := httptest.NewRequest(http.MethodPost, "/api/scheduler/availability", strings.NewReader(body))
	unauthenticatedResponse := httptest.NewRecorder()
	router.ServeHTTP(unauthenticatedResponse, unauthenticated)
	if unauthenticatedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticatedResponse.Code)
	}

	authenticated := httptest.NewRequest(http.MethodPost, "/api/scheduler/availability", strings.NewReader(body))
	authenticated.Header.Set("Authorization", "Bearer agent-secret")
	authenticatedResponse := httptest.NewRecorder()
	router.ServeHTTP(authenticatedResponse, authenticated)
	var response domain.AvailabilityResponse
	if err := json.NewDecoder(authenticatedResponse.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if authenticatedResponse.Code != http.StatusOK ||
		response.Outcome != domain.AvailabilityOutcomeNoAvailability ||
		response.ShouldRetrySameSearch ||
		response.Slots == nil {
		t.Fatalf("authenticated response = %d %#v", authenticatedResponse.Code, response)
	}
}

type schedulingStub struct {
	result       domain.AvailabilityResponse
	err          error
	bookResult   schedulingmodule.BookReceipt
	bookErr      error
	cancelResult schedulingmodule.CancelReceipt
	cancelErr    error
}

func (s schedulingStub) Search(context.Context, schedulingmodule.SearchCommand) (domain.AvailabilityResponse, error) {
	return s.result, s.err
}

func (s schedulingStub) Book(context.Context, schedulingmodule.BookCommand) (schedulingmodule.BookReceipt, error) {
	return s.bookResult, s.bookErr
}

func (s schedulingStub) Cancel(context.Context, schedulingmodule.CancelCommand) (schedulingmodule.CancelReceipt, error) {
	return s.cancelResult, s.cancelErr
}

func TestHandlePatientResolve_ValidationErrors(t *testing.T) {
	handlers := &Handlers{}

	tests := []struct {
		name        string
		method      string
		body        string
		expectedMsg string
	}{
		{
			name:        "invalid JSON",
			method:      "POST",
			body:        "not json",
			expectedMsg: "Invalid JSON body",
		},
		{
			name:        "missing lookup fields",
			method:      "POST",
			body:        `{"dob":"01/15/1980"}`,
			expectedMsg: "Provide patientId, phone, phone + firstName, phone + dob, or lastName + dob",
		},
		{
			name:        "missing dob",
			method:      "POST",
			body:        `{"lastName":"Smith"}`,
			expectedMsg: "Provide patientId, phone, phone + firstName, phone + dob, or lastName + dob",
		},
		{
			name:        "non-numeric patientId",
			method:      "POST",
			body:        `{"patientId":"abc"}`,
			expectedMsg: "patientId must be numeric",
		},
		{
			name:        "patientId with lookup fields",
			method:      "POST",
			body:        `{"patientId":"123","phone":"9542872010"}`,
			expectedMsg: "Provide either patientId or lookup fields, not both",
		},
		{
			name:        "phone without digits",
			method:      "POST",
			body:        `{"phone":"+"}`,
			expectedMsg: "phone must contain at least one digit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/patient/resolve", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandlePatientResolve(w, req)

			resp := w.Result()
			// Errors return 200 OK so ElevenLabs passes the body to the LLM
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var body PatientResolveResponse
			json.NewDecoder(resp.Body).Decode(&body)
			if body.Status != "error" {
				t.Errorf("Expected status 'error', got '%s'", body.Status)
			}
			if body.Message != tt.expectedMsg {
				t.Errorf("Expected message '%s', got '%s'", tt.expectedMsg, body.Message)
			}
		})
	}
}

func TestHandlePatientResolve_PhoneOnlyLoadsAppointments(t *testing.T) {
	handlers := newPatientResolveTestHandlers(t, http.StatusOK)

	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"phone":"9542872010","office":"Spring Hill"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandlePatientResolve(w, req)

	var body PatientResolveResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "verified" {
		t.Fatalf("status = %q, want verified; body = %+v", body.Status, body)
	}
	if body.PatientID != "123" {
		t.Fatalf("patientId = %q, want 123", body.PatientID)
	}
	if body.Name != "DOE,JANE" {
		t.Fatalf("name = %q, want DOE,JANE", body.Name)
	}
	if body.Phone != "850-373-3869" {
		t.Fatalf("phone = %q, want cell phone", body.Phone)
	}
	if body.AppointmentsStatus != string(patientmodule.AppointmentsFound) {
		t.Fatalf("appointmentsStatus = %q, want %q", body.AppointmentsStatus, patientmodule.AppointmentsFound)
	}
	if len(body.Appointments) != 1 {
		t.Fatalf("appointments = %+v, want one appointment", body.Appointments)
	}
	if body.Appointments[0].AppointmentTypeID != 1007 {
		t.Fatalf("appointmentTypeId = %d, want 1007", body.Appointments[0].AppointmentTypeID)
	}
}

func TestHandlePatientResolve_PhoneOnlyMultipleMatchesReturnsCandidatesWithoutHydration(t *testing.T) {
	var mu sync.Mutex
	demographicReads := 0
	appointmentReads := 0
	handlers := newPatientResolveTestHandlers(t, http.StatusOK, func(r *http.Request, body []byte) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(string(body), `"@action":"getdemographic"`):
			demographicReads++
		case strings.Contains(r.URL.Path, "/scheduler/appointments"):
			appointmentReads++
		}
	})

	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"phone":"5552223333","office":"Spring Hill"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandlePatientResolve(w, req)

	var body struct {
		Status  string           `json:"status"`
		Matches []map[string]any `json:"matches"`
	}
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "multiple_matches" {
		t.Fatalf("status = %q, want multiple_matches; body = %+v", body.Status, body)
	}
	if len(body.Matches) != 2 {
		t.Fatalf("matches = %+v, want two lightweight candidates", body.Matches)
	}
	want := []map[string]any{
		{"status": "candidate", "patientId": "123", "firstName": "JANE", "lastName": "DOE", "dob": "01/15/1980"},
		{"status": "candidate", "patientId": "456", "firstName": "JOHN", "lastName": "DOE", "dob": "03/20/1982"},
	}
	for i := range want {
		if !reflect.DeepEqual(body.Matches[i], want[i]) {
			t.Fatalf("match %d = %#v, want exact candidate %#v", i, body.Matches[i], want[i])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if demographicReads != 0 || appointmentReads != 0 {
		t.Fatalf("hydration reads = demographics:%d appointments:%d, want 0/0", demographicReads, appointmentReads)
	}
}

func TestHandlePatientResolve_PairedOfficeUsesEightProviderReads(t *testing.T) {
	var mu sync.Mutex
	patientSearches := 0
	demographicReads := 0
	appointmentColumns := make([]string, 0, 6)
	handlers := newPatientResolveTestHandlers(t, http.StatusOK, func(r *http.Request, body []byte) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(string(body), `"@action":"lookuppatient"`):
			patientSearches++
		case strings.Contains(string(body), `"@action":"getdemographic"`):
			demographicReads++
		case strings.Contains(r.URL.Path, "/scheduler/appointments"):
			appointmentColumns = append(appointmentColumns, r.URL.Query().Get("columnId"))
		}
	})

	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"phone":"9542872010","office":"Spring Hill"}`))
	w := httptest.NewRecorder()
	handlers.HandlePatientResolve(w, req)

	var body PatientResolveResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "verified" {
		t.Fatalf("response = %+v, want verified", body)
	}

	mu.Lock()
	defer mu.Unlock()
	if patientSearches != 1 || demographicReads != 1 || len(appointmentColumns) != 6 {
		t.Fatalf(
			"provider reads = search:%d demographics:%d appointments:%d, want 1/1/6",
			patientSearches,
			demographicReads,
			len(appointmentColumns),
		)
	}
	for _, columns := range appointmentColumns {
		if !strings.Contains(columns, "1513") || !strings.Contains(columns, "1593") {
			t.Fatalf("batched appointment columns = %q, want Spring Hill and Crystal River", columns)
		}
	}
}

func TestHandlePatientResolve_PatientIDRefreshUsesSameRoute(t *testing.T) {
	handlers := newPatientResolveTestHandlers(t, http.StatusOK)

	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"patientId":"123","office":"Spring Hill"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandlePatientResolve(w, req)

	var body PatientResolveResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "verified" {
		t.Fatalf("status = %q, want verified; body = %+v", body.Status, body)
	}
	if body.PatientID != "123" {
		t.Fatalf("patientId = %q, want 123", body.PatientID)
	}
	if body.AppointmentsStatus != string(patientmodule.AppointmentsFound) {
		t.Fatalf("appointmentsStatus = %q, want %q", body.AppointmentsStatus, patientmodule.AppointmentsFound)
	}
	if len(body.Appointments) != 1 {
		t.Fatalf("appointments = %+v, want one appointment", body.Appointments)
	}
}

func TestHandlePatientResolve_AppointmentFailureStillVerifies(t *testing.T) {
	handlers := newPatientResolveTestHandlers(t, http.StatusInternalServerError)

	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"phone":"9542872010","office":"Spring Hill"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandlePatientResolve(w, req)

	var body PatientResolveResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "verified" {
		t.Fatalf("status = %q, want verified; body = %+v", body.Status, body)
	}
	if body.AppointmentsStatus != string(patientmodule.AppointmentsError) {
		t.Fatalf("appointmentsStatus = %q, want %q", body.AppointmentsStatus, patientmodule.AppointmentsError)
	}
	if body.AppointmentsMessage == "" {
		t.Fatal("appointmentsMessage should explain the appointment lookup failure")
	}
	if strings.Contains(body.AppointmentsMessage, "status 500") {
		t.Fatalf("appointmentsMessage exposed provider error: %q", body.AppointmentsMessage)
	}
}

func TestPatientResolveLogsDoNotExposePatientIDOrProviderError(t *testing.T) {
	handlers := newPatientResolveTestHandlers(t, http.StatusInternalServerError)

	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"patientId":"17604634","office":"Spring Hill"}`))
	w := httptest.NewRecorder()
	handlers.HandlePatientResolve(w, req)

	got := logs.String()
	for _, forbidden := range []string{"17604634", "status 500", "appointment failure"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("logs exposed %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "patient-resolve: failed to get appointments category=upstream_status") {
		t.Fatalf("logs missing safe operation and category: %s", got)
	}
}

func TestProviderFailuresAreRedactedFromResponsesAndLogs(t *testing.T) {
	const sensitive = "patientId=17604634 token=secret-token provider-body=<private>"

	t.Run("add patient", func(t *testing.T) {
		handlers := newProviderFailureTestHandlers(t, func(r *http.Request, body []byte) *providerFailure {
			if !strings.Contains(string(body), `"@action":"addpatient"`) {
				return nil
			}
			return &providerFailure{status: http.StatusOK, body: fmt.Sprintf(`{"PPMDResults":{"Error":%q}}`, sensitive)}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/add-patient", strings.NewReader(`{
			"firstName":"Jane","lastName":"Doe","dob":"01/15/1980","phone":"9542872010",
			"street":"123 Main St","city":"Spring Hill","state":"FL","zip":"34609","sex":"female",
			"insurance":"Humana Medicare","subscriberName":"Jane Doe","subscriberNum":"H123","office":"Spring Hill"
		}`))
		w := httptest.NewRecorder()
		logs := captureLogs(func() { handlers.HandleAddPatient(w, req) })
		assertRedacted(t, w.Body.String(), logs, sensitive)
	})

	t.Run("update insurance", func(t *testing.T) {
		handlers := newProviderFailureTestHandlers(t, func(r *http.Request, body []byte) *providerFailure {
			if !strings.Contains(string(body), `"@action":"addinsurance"`) {
				return nil
			}
			return &providerFailure{status: http.StatusOK, body: fmt.Sprintf(`{"PPMDResults":{"Error":%q}}`, sensitive)}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/patient/update-insurance", strings.NewReader(`{
			"patientId":"17604634","insurance":"Humana Medicare","subscriberNum":"H123","office":"Spring Hill"
		}`))
		w := httptest.NewRecorder()
		logs := captureLogs(func() { handlers.HandleUpdateInsurance(w, req) })
		assertRedacted(t, w.Body.String(), logs, sensitive)
	})
}

func TestPatientMutationRoutesCallPatientInterface(t *testing.T) {
	service := &patientStub{
		createResult: patientmodule.CreateResult{
			Status:    patientmodule.CreateStatusCreated,
			PatientID: "123",
			Name:      "DOE,JANE",
			DOB:       "01/15/1980",
			Message:   "Patient created and insurance attached successfully",
		},
		updateResult: patientmodule.UpdateInsuranceResult{
			Status:       patientmodule.UpdateInsuranceStatusUpdated,
			PatientID:    "123",
			OldInsurance: "Old",
			NewInsurance: "Humana Medicare",
			Message:      "Insurance updated successfully",
		},
	}
	handlers := &Handlers{patient: service}

	t.Run("create", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/add-patient", strings.NewReader(`{
			"firstName":"Jane","lastName":"Doe","dob":"01/15/1980","phone":"9542872010",
			"street":"123 Main St","city":"Spring Hill","state":"FL","zip":"34609","sex":"female",
			"insurance":"Humana Medicare","subscriberName":"Jane Doe","subscriberNum":"H123","office":"Spring Hill"
		}`))
		w := httptest.NewRecorder()
		handlers.HandleAddPatient(w, req)

		if service.createCalls != 1 || service.createCommand.Office != "Spring Hill" {
			t.Fatalf("Create calls = %d command = %+v", service.createCalls, service.createCommand)
		}
		if strings.Contains(w.Body.String(), `"outcome"`) {
			t.Fatalf("successful response changed contract: %s", w.Body.String())
		}
	})

	t.Run("update insurance", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/patient/update-insurance", strings.NewReader(`{
			"patientId":"123","oldInsurance":"Old","insurance":"Humana Medicare",
			"subscriberNum":"H123","office":"Spring Hill"
		}`))
		w := httptest.NewRecorder()
		handlers.HandleUpdateInsurance(w, req)

		if service.updateCalls != 1 || service.updateCommand.PatientID != "123" {
			t.Fatalf("UpdateInsurance calls = %d command = %+v", service.updateCalls, service.updateCommand)
		}
		if strings.Contains(w.Body.String(), `"outcome"`) {
			t.Fatalf("successful response changed contract: %s", w.Body.String())
		}
	})
}

type patientStub struct {
	resolveResult patientmodule.ResolveResult
	resolveErr    error
	createResult  patientmodule.CreateResult
	updateResult  patientmodule.UpdateInsuranceResult
	createCommand patientmodule.CreateCommand
	updateCommand patientmodule.UpdateInsuranceCommand
	createCalls   int
	updateCalls   int
}

func (s *patientStub) Resolve(context.Context, patientmodule.ResolveCommand) (patientmodule.ResolveResult, error) {
	return s.resolveResult, s.resolveErr
}

func (s *patientStub) Create(_ context.Context, command patientmodule.CreateCommand) patientmodule.CreateResult {
	s.createCalls++
	s.createCommand = command
	return s.createResult
}

func (s *patientStub) UpdateInsurance(_ context.Context, command patientmodule.UpdateInsuranceCommand) patientmodule.UpdateInsuranceResult {
	s.updateCalls++
	s.updateCommand = command
	return s.updateResult
}

func captureLogs(run func()) string {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousWriter)
	run()
	return logs.String()
}

func assertRedacted(t *testing.T, response, logs string, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if strings.Contains(response, value) {
			t.Fatalf("response exposed %q: %s", value, response)
		}
		if strings.Contains(logs, value) {
			t.Fatalf("logs exposed %q: %s", value, logs)
		}
	}
	if !strings.Contains(logs, "category=") {
		t.Fatalf("logs missing safe error category: %s", logs)
	}
}

func TestHandleAddPatient_RoutineVisionRequiresOpticalOffice(t *testing.T) {
	handlers := &Handlers{patient: patientmodule.New(advancedmdtest.NewAdapter())}
	req := httptest.NewRequest("POST", "/api/add-patient", bytes.NewBufferString(`{
		"firstName":"Jane",
		"lastName":"Doe",
		"dob":"01/01/1980",
		"phone":"5551234567",
		"street":"123 Main St",
		"city":"Crystal River",
		"state":"FL",
		"zip":"34429",
		"sex":"female",
		"insurance":"VSP",
		"coverageType":"routine_vision",
		"subscriberName":"Jane Doe",
		"subscriberNum":"ABC123",
		"office":"+13523202007"
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleAddPatient(w, req)

	var body AddPatientResponse
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body.Status != "error" {
		t.Fatalf("expected status error, got %q", body.Status)
	}
	expected := "Routine vision coverage is not supported at Crystal River. Route the patient to Spring Hill routine vision scheduling."
	if body.Message != expected {
		t.Fatalf("expected message %q, got %q", expected, body.Message)
	}
}

func TestHandleAddPatient_RoutineOnlyOfficeRejectsMedical(t *testing.T) {
	handlers := &Handlers{patient: patientmodule.New(advancedmdtest.NewAdapter())}
	req := httptest.NewRequest("POST", "/api/add-patient", bytes.NewBufferString(`{
		"firstName":"Jane",
		"lastName":"Doe",
		"dob":"01/01/1980",
		"phone":"5551234567",
		"street":"123 Main St",
		"city":"North Miami Beach",
		"state":"FL",
		"zip":"33162",
		"sex":"female",
		"insurance":"Aetna",
		"subscriberName":"Jane Doe",
		"subscriberNum":"ABC123",
		"office":"+13055095333"
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleAddPatient(w, req)

	var body AddPatientResponse
	json.NewDecoder(w.Result().Body).Decode(&body)
	if body.Status != "error" {
		t.Fatalf("expected status error, got %q", body.Status)
	}
	expected := "Medical coverage is not supported at North Miami Beach Optical. Use routine vision coverage for this office or route medical visits to a medical office."
	if body.Message != expected {
		t.Fatalf("expected message %q, got %q", expected, body.Message)
	}
}

func TestAuthMiddleware(t *testing.T) {
	apiSecret := "test-secret-123"
	middleware := AuthMiddleware(apiSecret)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "no auth header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "wrong secret",
			authHeader:     "Bearer wrong-secret",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "valid bearer token",
			authHeader:     "Bearer test-secret-123",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid raw secret",
			authHeader:     "test-secret-123",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/patient/resolve", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request ID is in context
		requestID := GetRequestID(r.Context())
		if requestID == "" {
			t.Error("Expected request ID in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("generates new request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		requestID := w.Header().Get("X-Request-ID")
		if requestID == "" {
			t.Error("Expected X-Request-ID header")
		}
	})

	t.Run("uses existing request ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", "existing-id-123")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		requestID := w.Header().Get("X-Request-ID")
		if requestID != "existing-id-123" {
			t.Errorf("Expected 'existing-id-123', got '%s'", requestID)
		}
	})
}

func TestRouter(t *testing.T) {
	handlers := NewHandlers(nil, nil, nil)

	router := NewRouter(handlers, "test-secret", nil)

	t.Run("health endpoint no auth", func(t *testing.T) {
		tests := []struct {
			path       string
			wantStatus string
		}{
			{path: "/health", wantStatus: "ok"},
			{path: "/live", wantStatus: "ok"},
			{path: "/ready", wantStatus: "ready"},
		}
		for _, tt := range tests {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", tt.path, w.Code)
				continue
			}
			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Errorf("%s: decode response: %v", tt.path, err)
			} else if body["status"] != tt.wantStatus {
				t.Errorf("%s: status = %q, want %q", tt.path, body["status"], tt.wantStatus)
			}
		}
	})

	t.Run("api endpoints require auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"patientId":"123"}`))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 without auth, got %d", w.Code)
		}
	})

	t.Run("old patient read endpoints removed", func(t *testing.T) {
		for _, path := range []string{"/api/verify-patient", "/api/patient-lookup", "/api/patient/appointments"} {
			req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer test-secret")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected %s to be removed, got %d", path, w.Code)
			}
		}
	})

	t.Run("token endpoint removed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/token", nil)
		req.Header.Set("Authorization", "Bearer test-secret")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected removed token endpoint to be unavailable, got %d", w.Code)
		}
	})
}

func TestPatientApptDetail_IncludesID(t *testing.T) {
	detail := PatientApptDetail{
		ID:       9570263,
		Date:     "Wednesday, March 18, 2026",
		Time:     "12:00 PM",
		Provider: "Dr. Austin Bach",
		Type:     "New Adult Medical",
		Facility: "Abita Eye Group Spring Hill",
	}

	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	id, ok := decoded["id"]
	if !ok {
		t.Fatal("Expected 'id' field in JSON output")
	}
	if int(id.(float64)) != 9570263 {
		t.Errorf("Expected id 9570263, got %v", id)
	}
	if _, ok := decoded["confirmed"]; ok {
		t.Fatal("Did not expect 'confirmed' field in JSON output")
	}
}

func TestHandleUpdateInsurance_ValidationErrors(t *testing.T) {
	handlers := &Handlers{patient: patientmodule.New(advancedmdtest.NewAdapter())}

	tests := []struct {
		name        string
		body        string
		expectedMsg string
	}{
		{
			name:        "invalid JSON",
			body:        "not json",
			expectedMsg: "Invalid JSON body",
		},
		{
			name:        "missing patientId",
			body:        `{"insurance":"Aetna","subscriberNum":"ABC123"}`,
			expectedMsg: "patientId, insurance, and subscriberNum are required",
		},
		{
			name:        "missing insurance",
			body:        `{"patientId":"pat123","subscriberNum":"ABC123"}`,
			expectedMsg: "patientId, insurance, and subscriberNum are required",
		},
		{
			name:        "missing subscriberNum",
			body:        `{"patientId":"pat123","insurance":"Aetna"}`,
			expectedMsg: "patientId, insurance, and subscriberNum are required",
		},
		{
			name:        "insurance not recognized",
			body:        `{"patientId":"pat123","insurance":"FakeInsurance","subscriberNum":"ABC123"}`,
			expectedMsg: `Insurance not recognized: "FakeInsurance". Please use an insurance name from the accepted list.`,
		},
		{
			name:        "spring hill rejected medical plan",
			body:        `{"patientId":"pat123","insurance":"Cigna Local Plus","subscriberNum":"ABC123"}`,
			expectedMsg: "Cigna Local Plus is not accepted at Spring Hill.",
		},
		{
			name:        "crystal river rejected medical plan",
			body:        `{"patientId":"pat123","insurance":"Ambetter","subscriberNum":"ABC123","office":"+13523202007"}`,
			expectedMsg: "Ambetter is not accepted at Crystal River.",
		},
		{
			name:        "routine vision requires optical office",
			body:        `{"patientId":"pat123","insurance":"VSP","coverageType":"routine_vision","subscriberNum":"ABC123","office":"+13523202007"}`,
			expectedMsg: "Routine vision coverage is not supported at Crystal River. Route the patient to Spring Hill routine vision scheduling.",
		},
		{
			name:        "routine-only office rejects medical coverage",
			body:        `{"patientId":"pat123","insurance":"Aetna","subscriberNum":"ABC123","office":"+13055095333"}`,
			expectedMsg: "Medical coverage is not supported at North Miami Beach Optical. Use routine vision coverage for this office or route medical visits to a medical office.",
		},
		{
			name:        "invalid DOB",
			body:        `{"patientId":"pat123","insurance":"Aetna","subscriberNum":"ABC123","dob":"not-a-date"}`,
			expectedMsg: "dob must be a valid date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/patient/update-insurance", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleUpdateInsurance(w, req)

			var body UpdateInsuranceResponse
			json.NewDecoder(w.Result().Body).Decode(&body)
			if body.Status != "error" {
				t.Errorf("Expected status 'error', got %q", body.Status)
			}
			if body.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, body.Message)
			}
		})
	}
}

func TestHandleUpdateInsurance_SuccessRoutingAndDOB(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantRouting      string
		wantProviders    []string
		wantXMLRPCWrites int
	}{
		{
			name:             "routine vision filters age-restricted providers",
			body:             fmt.Sprintf(`{"patientId":"123","respPartyId":"resp123","insurance":"VSP","coverageType":"routine_vision","subscriberNum":"ABC123","office":"Hollywood","dob":%q}`, time.Now().AddDate(-6, 0, 0).Format("01/02/2006")),
			wantRouting:      string(domain.RoutingOpticalOnly),
			wantProviders:    []string{"Dr. Farnan", "Dr. Calero"},
			wantXMLRPCWrites: 1,
		},
		{
			name:             "routine vision accepts Sunshine Health alias",
			body:             fmt.Sprintf(`{"patientId":"123","respPartyId":"resp123","insurance":"Sunshine Health","coverageType":"routine_vision","subscriberNum":"ABC123","office":"Hollywood","dob":%q}`, time.Now().AddDate(-16, 0, 0).Format("01/02/2006")),
			wantRouting:      string(domain.RoutingOpticalOnly),
			wantProviders:    []string{"Dr. Farnan", "Dr. Vidal", "Dr. Calero"},
			wantXMLRPCWrites: 1,
		},
		{
			name:             "north miami beach optical routine vision",
			body:             `{"patientId":"123","respPartyId":"resp123","insurance":"VSP","coverageType":"routine_vision","subscriberNum":"ABC123","office":"+13055095333"}`,
			wantRouting:      string(domain.RoutingOpticalOnly),
			wantProviders:    []string{"Dr. Miriam Bach"},
			wantXMLRPCWrites: 1,
		},
		{
			name:             "medical minor uses pediatric routing",
			body:             fmt.Sprintf(`{"patientId":"123","respPartyId":"resp123","insPlanId":"ins123","oldInsurance":"Old","insurance":"Aetna","subscriberNum":"ABC123","office":"Spring Hill","dob":%q}`, time.Now().AddDate(-10, 0, 0).Format("01/02/2006")),
			wantRouting:      string(domain.RoutingBachOnly),
			wantProviders:    []string{"Dr. Bach"},
			wantXMLRPCWrites: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers, writes := newUpdateInsuranceTestHandlers(t)
			req := httptest.NewRequest("POST", "/api/patient/update-insurance", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleUpdateInsurance(w, req)

			var resp UpdateInsuranceResponse
			json.NewDecoder(w.Result().Body).Decode(&resp)
			if resp.Status != "updated" {
				t.Fatalf("expected updated response, got %#v", resp)
			}
			if resp.Routing != tt.wantRouting {
				t.Fatalf("routing = %q, want %q", resp.Routing, tt.wantRouting)
			}
			if len(resp.AllowedProviders) != len(tt.wantProviders) {
				t.Fatalf("allowedProviders = %v, want %v", resp.AllowedProviders, tt.wantProviders)
			}
			for i, want := range tt.wantProviders {
				if resp.AllowedProviders[i] != want {
					t.Fatalf("allowedProviders = %v, want %v", resp.AllowedProviders, tt.wantProviders)
				}
			}
			if len(*writes) != tt.wantXMLRPCWrites {
				t.Fatalf("XMLRPC writes = %d, want %d", len(*writes), tt.wantXMLRPCWrites)
			}
		})
	}
}

func TestHandleUpdateInsurance_SelfPayAutoSubscriberNum(t *testing.T) {
	handlers, writes := newUpdateInsuranceTestHandlers(t)
	req := httptest.NewRequest("POST", "/api/patient/update-insurance", bytes.NewBufferString(`{"patientId":"123","respPartyId":"resp123","insurance":"self-pay","office":"Spring Hill"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleUpdateInsurance(w, req)

	var resp UpdateInsuranceResponse
	json.NewDecoder(w.Result().Body).Decode(&resp)
	if resp.Status != "updated" {
		t.Fatalf("expected updated response, got %#v", resp)
	}
	if resp.Routing != string(domain.RoutingAll) {
		t.Fatalf("routing = %q, want %q", resp.Routing, domain.RoutingAll)
	}
	if len(*writes) != 1 {
		t.Fatalf("XMLRPC writes = %d, want 1", len(*writes))
	}
	if !strings.Contains((*writes)[0], "car301672") || !strings.Contains((*writes)[0], "self pay") {
		t.Fatalf("self-pay addinsurance payload = %s", (*writes)[0])
	}
}

func TestHandleUpdateInsurance_AetnaGovernmentVisionUsesICareCarrier(t *testing.T) {
	handlers, writes := newUpdateInsuranceTestHandlers(t)
	req := httptest.NewRequest("POST", "/api/patient/update-insurance", bytes.NewBufferString(`{"patientId":"123","respPartyId":"resp123","insurance":"Aetna Dual Eligible Medicare Advantage","coverageType":"routine_vision","subscriberNum":"ABC123","office":"Hollywood"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleUpdateInsurance(w, req)

	var resp UpdateInsuranceResponse
	json.NewDecoder(w.Result().Body).Decode(&resp)
	if resp.Status != "updated" {
		t.Fatalf("expected updated response, got %#v", resp)
	}
	if resp.Routing != string(domain.RoutingOpticalOnly) {
		t.Fatalf("routing = %q, want %q", resp.Routing, domain.RoutingOpticalOnly)
	}
	if len(*writes) != 1 {
		t.Fatalf("XMLRPC writes = %d, want 1", len(*writes))
	}
	if !strings.Contains((*writes)[0], "car40907") {
		t.Fatalf("Aetna government vision payload = %s, want iCare carrier car40907", (*writes)[0])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type providerFailure struct {
	status int
	body   string
}

func newProviderFailureTestHandlers(t *testing.T, fail func(*http.Request, []byte) *providerFailure) *Handlers {
	t.Helper()
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			contentType := r.Header.Get("Content-Type")

			status := http.StatusOK
			response := `{"PPMDResults":{"Results":{}}}`
			switch {
			case strings.Contains(contentType, "application/xml") && strings.Contains(r.URL.Host, "partnerlogin"):
				response = `<PPMDResults><Results><usercontext webserver="https://mock.advancedmd.test/processrequest/api-801/APP"></usercontext></Results></PPMDResults>`
			case strings.Contains(contentType, "application/xml"):
				response = `<PPMDResults><Results success="1"><usercontext>test-token</usercontext></Results></PPMDResults>`
			default:
				if failure := fail(r, body); failure != nil {
					status = failure.status
					response = failure.body
				}
			}

			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(response)),
				Request:    r,
			}, nil
		}),
	}
	amdSession := session.NewSession(session.Credentials{
		Username: "user", Password: "pass", OfficeKey: "office", AppName: "app",
	}, httpClient)

	records := advancedmd.NewAdapter(
		amdSession,
		clients.NewAdvancedMDClient(httpClient),
		clients.NewAdvancedMDRestClient(httpClient),
	)
	return NewHandlers(amdSession, patientmodule.New(records), nil)
}

func newUpdateInsuranceTestHandlers(t *testing.T) (*Handlers, *[]string) {
	t.Helper()
	writes := []string{}
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			contentType := r.Header.Get("Content-Type")

			var response string
			switch {
			case strings.Contains(contentType, "application/xml") && strings.Contains(r.URL.Host, "partnerlogin"):
				response = `<PPMDResults><Results><usercontext webserver="https://mock.advancedmd.test/processrequest/api-801/APP"></usercontext></Results></PPMDResults>`
			case strings.Contains(contentType, "application/xml"):
				response = `<PPMDResults><Results success="1"><usercontext>test-token</usercontext></Results></PPMDResults>`
			default:
				writes = append(writes, string(body))
				response = `{"PPMDResults":{"Results":{}}}`
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(response)),
				Request:    r,
			}, nil
		}),
	}

	amdSession := session.NewSession(session.Credentials{
		Username:  "user",
		Password:  "pass",
		OfficeKey: "office",
		AppName:   "app",
	}, httpClient)

	records := advancedmd.NewAdapter(
		amdSession,
		clients.NewAdvancedMDClient(httpClient),
		clients.NewAdvancedMDRestClient(httpClient),
	)
	return NewHandlers(amdSession, patientmodule.New(records), nil), &writes
}

func newPatientResolveTestHandlers(
	t *testing.T,
	appointmentStatus int,
	observers ...func(*http.Request, []byte),
) *Handlers {
	t.Helper()
	eastern := domain.EasternLocation()
	future := time.Now().In(eastern).Add(48 * time.Hour)
	futureMonth := time.Date(future.Year(), future.Month(), 1, 0, 0, 0, 0, eastern).Format("2006-01-02")

	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(r.Body)
			}
			for _, observe := range observers {
				observe(r, body)
			}
			contentType := r.Header.Get("Content-Type")

			status := http.StatusOK
			response := `{}`
			switch {
			case strings.Contains(contentType, "application/xml") && strings.Contains(r.URL.Host, "partnerlogin"):
				response = `<PPMDResults><Results><usercontext webserver="https://mock.advancedmd.test/processrequest/api-801/APP"></usercontext></Results></PPMDResults>`
			case strings.Contains(contentType, "application/xml"):
				response = `<PPMDResults><Results success="1"><usercontext>test-token</usercontext></Results></PPMDResults>`
			case strings.Contains(r.URL.Path, "/scheduler/appointments"):
				status = appointmentStatus
				columnID := r.URL.Query().Get("columnId")
				if status == http.StatusOK && r.URL.Query().Get("startDate") == futureMonth && strings.Contains(columnID, "1513") {
					response = fmt.Sprintf(`[{
						"id": 9570263,
						"startdatetime": %q,
						"patientid": 123,
						"columnid": 1513,
						"profile": "BACH, AUSTIN",
						"facility": "ABITA EYE GROUP SPRING HILL",
						"appointmenttypeids": [1007]
					}]`, future.Format("2006-01-02T15:04:05"))
				} else if status == http.StatusOK {
					response = `[]`
				} else {
					response = `{"error":"appointment failure"}`
				}
			case strings.Contains(string(body), `"@action":"lookuppatient"`) && strings.Contains(string(body), `"@phone":"5552223333"`):
				response = `{
					"PPMDResults": {
						"Results": {
							"patientlist": {
								"@itemcount": "2",
								"patient": [
									{
										"@id": "pat123",
										"@name": "DOE,JANE",
										"@dob": "01/15/1980",
										"contactinfo": {"@cellphone": "850-373-3869"}
									},
									{
										"@id": "pat456",
										"@name": "DOE,JOHN",
										"@dob": "03/20/1982",
										"contactinfo": {"@cellphone": "850-373-0000"}
									}
								]
							}
						}
					}
				}`
			case strings.Contains(string(body), `"@action":"lookuppatient"`):
				response = `{
					"PPMDResults": {
						"Results": {
							"patientlist": {
								"@itemcount": "1",
								"patient": {
									"@id": "pat123",
									"@name": "DOE,JANE",
									"@dob": "01/15/1980",
									"contactinfo": {"@cellphone": "850-373-3869"}
								}
							}
						}
					}
				}`
			case strings.Contains(string(body), `"@action":"getdemographic"`):
				response = `{
					"PPMDResults": {
						"Results": {
							"patientlist": {
									"patient": {
										"@id": "pat123",
										"@name": "DOE,JANE",
										"@respparty": "resp456",
									"@dob": "01/15/1980",
									"insplanlist": {
										"insplan": {
											"@id": "ins789",
											"@carrier": "car40906",
											"@subscriber": "resp456",
											"@enddate": "",
											"@coverage": "1"
										}
									}
								}
							},
							"carrierlist": {
								"carrier": {
									"@id": "car40906",
									"@name": "HUMANA MEDICARE"
								}
							}
						}
					}
				}`
			}

			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(response)),
				Request:    r,
			}, nil
		}),
	}

	amdSession := session.NewSession(session.Credentials{
		Username:  "user",
		Password:  "pass",
		OfficeKey: "office",
		AppName:   "app",
	}, httpClient)
	amdClient := clients.NewAdvancedMDClient(httpClient)
	amdRestClient := clients.NewAdvancedMDRestClient(httpClient)
	records := advancedmd.NewAdapter(amdSession, amdClient, amdRestClient)

	return NewHandlers(amdSession, patientmodule.New(records), nil)
}

func (s schedulingStub) List(ctx context.Context, command schedulingmodule.ListCommand) (domain.AvailabilityResponse, error) {
	return s.Search(ctx, schedulingmodule.SearchCommand{Office: command.Office})
}
