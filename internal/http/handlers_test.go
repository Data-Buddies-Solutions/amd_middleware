package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/auth"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
)

func TestHandleHealth(t *testing.T) {
	handlers := &Handlers{}

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handlers.HandleHealth(w, req)

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

func TestBuildBookAppointmentReceipt(t *testing.T) {
	office := &domain.OfficeConfig{
		DisplayName: "Spring Hill",
		Columns: map[string]domain.OfficeColumn{
			"1513": {
				ProfileID:   "620",
				DisplayName: "Dr. Austin Bach",
			},
		},
	}
	req := BookAppointmentRequest{
		PatientID:         "12345",
		PatientName:       "SMITH,JANE",
		ColumnID:          1513,
		ProfileID:         620,
		StartDatetime:     "2026-05-12T11:00",
		Duration:          30,
		AppointmentTypeID: 1007,
	}

	receipt := buildBookAppointmentReceipt(req, office, 98765)

	if receipt.Status != "booked" {
		t.Fatalf("expected status booked, got %q", receipt.Status)
	}
	if receipt.AppointmentID != 98765 {
		t.Errorf("expected appointment ID 98765, got %d", receipt.AppointmentID)
	}
	if receipt.PatientID != "12345" {
		t.Errorf("expected patient ID 12345, got %q", receipt.PatientID)
	}
	if receipt.PatientName != "Jane Smith" {
		t.Errorf("expected patient name Jane Smith, got %q", receipt.PatientName)
	}
	if receipt.ProviderName != "Dr. Austin Bach" {
		t.Errorf("expected provider name Dr. Austin Bach, got %q", receipt.ProviderName)
	}
	if receipt.LocationName != "Spring Hill" {
		t.Errorf("expected location Spring Hill, got %q", receipt.LocationName)
	}
	if receipt.StartDatetime != "2026-05-12T11:00" {
		t.Errorf("expected start datetime to be echoed, got %q", receipt.StartDatetime)
	}
	if receipt.Duration != 30 {
		t.Errorf("expected duration 30, got %d", receipt.Duration)
	}
	if receipt.AppointmentTypeID != 1007 {
		t.Errorf("expected appointment type ID 1007, got %d", receipt.AppointmentTypeID)
	}
	if receipt.AppointmentTypeName != "Established Adult Medical (Follow Up)" {
		t.Errorf("expected appointment type name Established Adult Medical (Follow Up), got %q", receipt.AppointmentTypeName)
	}
}

func TestFilterColumnsForRouting_RoutineVisionLane(t *testing.T) {
	office := domain.DefaultOffice()
	columns := []domain.SchedulerColumn{
		{ID: "1513"},
		{ID: "1598"},
		{ID: "1551"},
		{ID: "1550"},
		{ID: "1600"},
	}

	medical := filterColumnsForRouting(columns, office, domain.ParseRoutingRule(""))
	if len(medical) != 4 {
		t.Fatalf("default medical routing returned %d columns, want 4", len(medical))
	}
	for _, col := range medical {
		if col.ID == "1600" {
			t.Fatal("default medical routing should not include routine vision column 1600")
		}
	}

	optical := filterColumnsForRouting(columns, office, domain.ParseRoutingRule("optical_only"))
	if len(optical) != 1 {
		t.Fatalf("optical_only routing returned %d columns, want 1", len(optical))
	}
	if optical[0].ID != "1600" {
		t.Fatalf("optical_only routing returned column %s, want 1600", optical[0].ID)
	}
}

func TestFilterColumnsForDOB_RoutineAgeRules(t *testing.T) {
	office, ok := domain.LookupOffice("+19542872010")
	if !ok {
		t.Fatal("expected Hollywood office")
	}
	columns := []domain.SchedulerColumn{
		{ID: "1555"},
		{ID: "1510"},
		{ID: "1305"},
	}

	if filtered := filterColumnsForDOB(columns, office, ""); len(filtered) != 0 {
		t.Fatalf("missing DOB filtered columns = %v, want none", filtered)
	}
	if filtered := filterColumnsForDOB(columns, office, "not-a-date"); len(filtered) != 0 {
		t.Fatalf("invalid DOB filtered columns = %v, want none", filtered)
	}

	dob := time.Now().AddDate(-4, 0, 0).Format("01/02/2006")

	filtered := filterColumnsForDOB(columns, office, dob)
	if len(filtered) != 1 {
		t.Fatalf("filtered columns = %v, want only Calero", filtered)
	}
	if filtered[0].ID != "1305" {
		t.Fatalf("filtered column = %s, want 1305", filtered[0].ID)
	}
}

func TestHandleBookAppointment_RoutingGuard(t *testing.T) {
	handlers := &Handlers{}

	tests := []struct {
		name        string
		body        string
		expectedMsg string
	}{
		{
			name:        "routine vision column requires optical routing",
			body:        `{"patientId":"123","columnId":1600,"profileId":1983,"startDatetime":"2026-05-12T10:00","duration":45,"appointmentTypeId":1007}`,
			expectedMsg: `Column 1600 is not valid for routing "all_three" at Spring Hill`,
		},
		{
			name:        "medical column rejected for optical routing",
			body:        `{"patientId":"123","columnId":1513,"profileId":620,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1007,"routing":"optical_only"}`,
			expectedMsg: `Column 1513 is not valid for routing "optical_only" at Spring Hill`,
		},
		{
			name:        "routine vision column rejects mismatched profile",
			body:        `{"patientId":"123","columnId":1600,"profileId":620,"startDatetime":"2026-05-12T10:00","duration":45,"appointmentTypeId":1010,"routing":"optical_only"}`,
			expectedMsg: `Profile 620 is not valid for column 1600 at Spring Hill`,
		},
		{
			name:        "routine vision routing requires vision appointment type",
			body:        `{"patientId":"123","columnId":1600,"profileId":1983,"startDatetime":"2026-05-12T10:00","duration":45,"appointmentTypeId":1007,"routing":"optical_only"}`,
			expectedMsg: `Appointment type 1007 is not valid for routing "optical_only" at Spring Hill`,
		},
		{
			name:        "vision appointment type rejected for medical routing",
			body:        `{"patientId":"123","columnId":1513,"profileId":620,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1010}`,
			expectedMsg: `Appointment type 1010 is not valid for routing "all_three" at Spring Hill`,
		},
		{
			name:        "crystal river type rejected at spring hill",
			body:        `{"patientId":"123","columnId":1513,"profileId":620,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":6169}`,
			expectedMsg: `Appointment type 6169 is not valid for routing "all_three" at Spring Hill`,
		},
		{
			name:        "hollywood routine rejects medical type",
			body:        `{"patientId":"123","columnId":1555,"profileId":2075,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1007,"routing":"optical_only","office":"+19542872010"}`,
			expectedMsg: `Appointment type 1007 is not valid for routing "optical_only" at Hollywood`,
		},
		{
			name:        "hollywood medical rejects vision type",
			body:        `{"patientId":"123","columnId":1268,"profileId":620,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1010,"office":"+19542872010"}`,
			expectedMsg: `Appointment type 1010 is not valid for routing "all_three" at Hollywood`,
		},
		{
			name:        "invalid DOB rejected before AMD call",
			body:        `{"patientId":"123","columnId":1513,"profileId":620,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1007,"dob":"not-a-date"}`,
			expectedMsg: `dob must be a valid date`,
		},
		{
			name:        "minor medical booking uses pediatric routing",
			body:        fmt.Sprintf(`{"patientId":"123","columnId":1551,"profileId":2064,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1007,"dob":%q}`, time.Now().AddDate(-10, 0, 0).Format("01/02/2006")),
			expectedMsg: `Column 1551 is not valid for routing "bach_only" at Spring Hill`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/appointment/book", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleBookAppointment(w, req)

			var body BookAppointmentResponse
			json.NewDecoder(w.Result().Body).Decode(&body)
			if body.Status != "error" {
				t.Fatalf("expected status error, got %q", body.Status)
			}
			if body.Message != tt.expectedMsg {
				t.Fatalf("expected message %q, got %q", tt.expectedMsg, body.Message)
			}
		})
	}
}

func TestHandleBookAppointment_AgeGuard(t *testing.T) {
	handlers := &Handlers{}

	underageDOB := time.Now().AddDate(-6, 0, 0).Format("01/02/2006")
	tests := []struct {
		name        string
		dobFragment string
		expectedMsg string
	}{
		{
			name:        "under minimum age",
			dobFragment: fmt.Sprintf(`,"dob":%q`, underageDOB),
			expectedMsg: "Dr. Vidal requires patient age 7 or older",
		},
		{
			name:        "missing DOB",
			dobFragment: "",
			expectedMsg: "Dr. Vidal requires patient DOB to verify age 7 or older",
		},
		{
			name:        "invalid DOB",
			dobFragment: `,"dob":"not-a-date"`,
			expectedMsg: "dob must be a valid date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"patientId":"123","columnId":1510,"profileId":2057,"startDatetime":"2026-05-12T10:00","duration":30,"appointmentTypeId":1010,"routing":"optical_only","office":"+19542872010"%s}`, tt.dobFragment)
			req := httptest.NewRequest("POST", "/api/appointment/book", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleBookAppointment(w, req)

			var resp BookAppointmentResponse
			json.NewDecoder(w.Result().Body).Decode(&resp)
			if resp.Status != "error" {
				t.Fatalf("expected status error, got %q", resp.Status)
			}
			if resp.Message != tt.expectedMsg {
				t.Fatalf("expected message %q, got %q", tt.expectedMsg, resp.Message)
			}
		})
	}
}

func TestHandleGetAvailability_InvalidDOB(t *testing.T) {
	handlers := &Handlers{}
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	body := fmt.Sprintf(`{"date":%q,"office":"Hollywood","routing":"optical_only","dob":"not-a-date"}`, date)
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

func TestEffectiveRoutingForDOB(t *testing.T) {
	office := domain.DefaultOffice()
	minorDOB := time.Now().AddDate(-10, 0, 0).Format("01/02/2006")
	adultDOB := time.Now().AddDate(-30, 0, 0).Format("01/02/2006")

	if got := effectiveRoutingForDOB(office, domain.RoutingAll, minorDOB); got != domain.RoutingBachOnly {
		t.Fatalf("minor medical routing = %q, want %q", got, domain.RoutingBachOnly)
	}
	if got := effectiveRoutingForDOB(office, domain.RoutingAll, adultDOB); got != domain.RoutingAll {
		t.Fatalf("adult medical routing = %q, want %q", got, domain.RoutingAll)
	}
	if got := effectiveRoutingForDOB(office, domain.RoutingOpticalOnly, minorDOB); got != domain.RoutingOpticalOnly {
		t.Fatalf("routine vision routing = %q, want %q", got, domain.RoutingOpticalOnly)
	}
}

func TestHandleVerifyPatient_ValidationErrors(t *testing.T) {
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
			name:        "missing lastName and phone",
			method:      "POST",
			body:        `{"dob":"01/15/1980"}`,
			expectedMsg: "Provide phone + firstName, phone + dob, or lastName + dob",
		},
		{
			name:        "missing dob",
			method:      "POST",
			body:        `{"lastName":"Smith"}`,
			expectedMsg: "Provide phone + firstName, phone + dob, or lastName + dob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/verify-patient", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleVerifyPatient(w, req)

			resp := w.Result()
			// Errors return 200 OK so ElevenLabs passes the body to the LLM
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var body VerifyPatientResponse
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

func TestAddPatientMissingFields_EmailOptional(t *testing.T) {
	baseReq := AddPatientRequest{
		FirstName:      "Jane",
		LastName:       "Doe",
		DOB:            "2000-03-01",
		Phone:          "555-123-4567",
		Street:         "123 Main St",
		City:           "Miami",
		State:          "FL",
		Zip:            "33101",
		Sex:            "F",
		Insurance:      "Aetna",
		SubscriberName: "Jane Doe",
		SubscriberNum:  "A12345",
	}

	tests := []struct {
		name  string
		email string
	}{
		{name: "omitted", email: ""},
		{name: "blank", email: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := baseReq
			req.Email = tt.email

			missing := addPatientMissingFields(req)
			if len(missing) != 0 {
				t.Fatalf("Expected no missing fields when email is %s, got %v", tt.name, missing)
			}
		})
	}
}

func TestHandleAddPatient_RoutineVisionRequiresOpticalOffice(t *testing.T) {
	handlers := &Handlers{}
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
			req := httptest.NewRequest("GET", "/api/token", nil)
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

func TestCalculateAvailableSlots_AllBlocked(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	// Use a future Monday so it's not "today"
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, eastern)
	nowEastern := time.Date(2026, 3, 3, 10, 0, 0, 0, eastern)

	col := domain.SchedulerColumn{
		ID:              "1513",
		Name:            "DR. BACH - BP",
		StartTime:       "08:00",
		EndTime:         "17:00",
		Interval:        15,
		MaxApptsPerSlot: 0,
		Workweek:        62, // Mon-Fri
	}

	// Block hold covering the entire work day
	blockHolds := []domain.BlockHold{
		{
			StartDateTime: time.Date(2026, 6, 1, 8, 0, 0, 0, eastern),
			EndDateTime:   time.Date(2026, 6, 1, 17, 0, 0, 0, eastern),
			Note:          "OUT OF THE OFFICE",
		},
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, nil, blockHolds, date, nowEastern)

	if len(slots) != 0 {
		t.Errorf("Expected 0 slots when entire day is blocked, got %d", len(slots))
	}
}

func TestCalculateAvailableSlots_AllBookedAtMax(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	nowEastern := time.Date(2026, 3, 3, 10, 0, 0, 0, eastern)

	col := domain.SchedulerColumn{
		ID:              "1551",
		Name:            "DR. LICHT",
		StartTime:       "09:00",
		EndTime:         "10:00",
		Interval:        15,
		MaxApptsPerSlot: 2,  // Max 2 per slot
		Workweek:        24, // Wed-Thu
	}

	// June 3 2026 is a Wednesday
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, eastern)

	// Fill every slot with 2 appointments
	var appointments []domain.Appointment
	for h := 9; h < 10; h++ {
		for m := 0; m < 60; m += 15 {
			for i := 0; i < 2; i++ {
				appointments = append(appointments, domain.Appointment{
					StartDateTime: time.Date(2026, 6, 3, h, m, 0, 0, eastern),
					Duration:      15,
				})
			}
		}
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, appointments, nil, date, nowEastern)

	if len(slots) != 0 {
		t.Errorf("Expected 0 slots when all slots at max capacity, got %d", len(slots))
	}
}

func TestNoAvailabilityResponse_HasExplicitNoRetryContract(t *testing.T) {
	resp := domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusSuccess,
		Outcome:               domain.AvailabilityOutcomeNoAvailability,
		AvailabilityFound:     false,
		RequestedDate:         "2026-05-15",
		ShouldRetrySameSearch: false,
		NextAction:            domain.AvailabilityNextActionAskDifferentPreferences,
		SearchedFrom:          "2026-05-15",
		SearchedThrough:       "2026-05-29",
		Message:               noAvailabilityMessage("2026-05-15", "2026-05-29"),
		Slots:                 []domain.AvailabilitySlotOption{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["status"] != domain.AvailabilityStatusSuccess {
		t.Errorf("Expected success status, got %q", decoded["status"])
	}
	if decoded["outcome"] != domain.AvailabilityOutcomeNoAvailability {
		t.Errorf("Expected no-availability outcome, got %q", decoded["outcome"])
	}
	if decoded["availabilityFound"] != false {
		t.Errorf("Expected availabilityFound false, got %v", decoded["availabilityFound"])
	}
	if decoded["shouldRetrySameSearch"] != false {
		t.Errorf("Expected shouldRetrySameSearch false, got %v", decoded["shouldRetrySameSearch"])
	}
	if decoded["nextAction"] != domain.AvailabilityNextActionAskDifferentPreferences {
		t.Errorf("Expected ask-preferences next action, got %q", decoded["nextAction"])
	}
	if decoded["searchedFrom"] != "2026-05-15" {
		t.Errorf("Expected searchedFrom 2026-05-15, got %q", decoded["searchedFrom"])
	}
	if decoded["searchedThrough"] != "2026-05-29" {
		t.Errorf("Expected searchedThrough 2026-05-29, got %q", decoded["searchedThrough"])
	}
	if !strings.Contains(decoded["message"].(string), "2026-05-15 through 2026-05-29") {
		t.Errorf("Expected no-availability message, got %q", decoded["message"])
	}
	slots := decoded["slots"].([]interface{})
	if len(slots) != 0 {
		t.Errorf("Expected empty slots array, got %d", len(slots))
	}
}

func TestAvailabilityResponse_HasFoundContractAndOmitsMessageWhenEmpty(t *testing.T) {
	resp := domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusSuccess,
		Outcome:               domain.AvailabilityOutcomeFound,
		AvailabilityFound:     true,
		RequestedDate:         "2026-05-18",
		ShouldRetrySameSearch: false,
		NextAction:            domain.AvailabilityNextActionOfferSlots,
		ActualDate:            "2026-06-01",
		DateShifted:           true,
		Slots: []domain.AvailabilitySlotOption{
			{
				Provider:  "Dr. Kyler Farnan",
				Time:      "8:30 AM",
				DateTime:  "2026-06-01T08:30",
				ColumnID:  1555,
				ProfileID: 2075,
				Duration:  15,
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["status"] != domain.AvailabilityStatusSuccess {
		t.Errorf("Expected success status, got %q", decoded["status"])
	}
	if decoded["outcome"] != domain.AvailabilityOutcomeFound {
		t.Errorf("Expected availability-found outcome, got %q", decoded["outcome"])
	}
	if decoded["availabilityFound"] != true {
		t.Errorf("Expected availabilityFound true, got %v", decoded["availabilityFound"])
	}
	if decoded["nextAction"] != domain.AvailabilityNextActionOfferSlots {
		t.Errorf("Expected offer-slots next action, got %q", decoded["nextAction"])
	}
	if decoded["actualDate"] != "2026-06-01" {
		t.Errorf("Expected actualDate 2026-06-01, got %q", decoded["actualDate"])
	}
	if decoded["dateShifted"] != true {
		t.Errorf("Expected dateShifted true, got %v", decoded["dateShifted"])
	}
	if decoded["shouldRetrySameSearch"] != false {
		t.Errorf("Expected shouldRetrySameSearch false, got %v", decoded["shouldRetrySameSearch"])
	}
	slots := decoded["slots"].([]interface{})
	if len(slots) != 1 {
		t.Fatalf("Expected one slot, got %d", len(slots))
	}
	slot := slots[0].(map[string]interface{})
	if slot["provider"] != "Dr. Kyler Farnan" || slot["datetime"] != "2026-06-01T08:30" {
		t.Errorf("Unexpected slot payload: %v", slot)
	}
	if _, exists := decoded["message"]; exists {
		t.Error("Expected message field to be omitted when empty")
	}
}

func TestIncompleteAvailabilityResponse_AllowsRetry(t *testing.T) {
	resp := domain.AvailabilityResponse{
		Status:                domain.AvailabilityStatusError,
		Outcome:               domain.AvailabilityOutcomeSearchIncomplete,
		AvailabilityFound:     false,
		RequestedDate:         "2026-05-15",
		ShouldRetrySameSearch: true,
		NextAction:            domain.AvailabilityNextActionRetryOnceThenAskPreferences,
		SearchedFrom:          "2026-05-15",
		SearchedThrough:       "2026-05-29",
		Message:               incompleteAvailabilityMessage("2026-05-15", "2026-05-29", 3),
		Slots:                 []domain.AvailabilitySlotOption{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["status"] != domain.AvailabilityStatusError {
		t.Errorf("Expected error status, got %q", decoded["status"])
	}
	if decoded["outcome"] != domain.AvailabilityOutcomeSearchIncomplete {
		t.Errorf("Expected search-incomplete outcome, got %q", decoded["outcome"])
	}
	if decoded["shouldRetrySameSearch"] != true {
		t.Errorf("Expected shouldRetrySameSearch true, got %v", decoded["shouldRetrySameSearch"])
	}
	if decoded["nextAction"] != domain.AvailabilityNextActionRetryOnceThenAskPreferences {
		t.Errorf("Expected retry-then-ask-preferences next action, got %q", decoded["nextAction"])
	}
	if !strings.Contains(decoded["message"].(string), "ask for different preferences") {
		t.Errorf("Expected incomplete-search message, got %q", decoded["message"])
	}
}

func TestFlattenAvailabilitySlots(t *testing.T) {
	providers := []domain.ProviderAvailability{
		{
			Name:         "Dr. Kyler Farnan",
			ColumnID:     1555,
			ProfileID:    2075,
			SlotDuration: 15,
			Slots: []domain.AvailableSlot{
				{Time: "8:30 AM", DateTime: "2026-06-01T08:30", SameStartBooked: 1, SameStartCapacity: 2, RequiresForce: true},
				{Time: "8:45 AM", DateTime: "2026-06-01T08:45"},
			},
			TotalAvailable: 2,
		},
	}

	slots := flattenAvailabilitySlots(providers)
	if len(slots) != 2 {
		t.Fatalf("slots = %d, want 2", len(slots))
	}
	if slots[0].Provider != "Dr. Kyler Farnan" ||
		slots[0].DateTime != "2026-06-01T08:30" ||
		slots[0].ColumnID != 1555 ||
		slots[0].ProfileID != 2075 ||
		slots[0].Duration != 15 ||
		slots[0].SameStartBooked != 1 ||
		slots[0].SameStartCapacity != 2 ||
		!slots[0].RequiresForce {
		t.Fatalf("unexpected flattened slot: %+v", slots[0])
	}
}

func TestBookingTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	payload := bookingTokenPayload{
		OfficeID:      "spring_hill",
		Routing:       string(domain.RoutingAll),
		ColumnID:      1513,
		ProfileID:     620,
		StartDatetime: "2026-06-02T09:00",
		Duration:      15,
		Provider:      "Dr. Austin Bach",
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(bookingTokenTTL).Unix(),
	}

	token, err := signBookingToken("test-booking-secret", payload)
	if err != nil {
		t.Fatalf("signBookingToken error = %v", err)
	}

	got, err := verifyBookingToken("test-booking-secret", token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("verifyBookingToken error = %v", err)
	}
	if got.OfficeID != payload.OfficeID ||
		got.Routing != payload.Routing ||
		got.ColumnID != payload.ColumnID ||
		got.ProfileID != payload.ProfileID ||
		got.StartDatetime != payload.StartDatetime ||
		got.Duration != payload.Duration {
		t.Fatalf("decoded payload = %+v, want %+v", got, payload)
	}
}

func TestBookingTokenRejectsTamperedExpiredAndWrongOffice(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	payload := bookingTokenPayload{
		OfficeID:      "spring_hill",
		Routing:       string(domain.RoutingAll),
		ColumnID:      1513,
		ProfileID:     620,
		StartDatetime: "2026-06-02T09:00",
		Duration:      15,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(bookingTokenTTL).Unix(),
	}
	token, err := signBookingToken("test-booking-secret", payload)
	if err != nil {
		t.Fatalf("signBookingToken error = %v", err)
	}

	if _, err := verifyBookingToken("wrong-secret", token, now); err == nil {
		t.Fatal("wrong secret should reject token")
	}
	if _, err := verifyBookingToken("test-booking-secret", token+"x", now); err == nil {
		t.Fatal("tampered token should be rejected")
	}
	if _, err := verifyBookingToken("test-booking-secret", token, now.Add(bookingTokenTTL)); err == nil {
		t.Fatal("expired token should be rejected")
	}

	oversizedTTL := payload
	oversizedTTL.ExpiresAt = now.Add(bookingTokenTTL + time.Second).Unix()
	oversizedToken, err := signBookingToken("test-booking-secret", oversizedTTL)
	if err != nil {
		t.Fatalf("sign oversized TTL token: %v", err)
	}
	if _, err := verifyBookingToken("test-booking-secret", oversizedToken, now); err == nil {
		t.Fatal("token with oversized TTL should be rejected")
	}

	futureIssued := payload
	futureIssued.IssuedAt = now.Add(bookingTokenClockSkew + time.Second).Unix()
	futureIssued.ExpiresAt = now.Add(bookingTokenClockSkew + time.Minute).Unix()
	futureToken, err := signBookingToken("test-booking-secret", futureIssued)
	if err != nil {
		t.Fatalf("sign future issued token: %v", err)
	}
	if _, err := verifyBookingToken("test-booking-secret", futureToken, now); err == nil {
		t.Fatal("future-issued token should be rejected")
	}

	req := BookAppointmentRequest{BookingToken: token}
	handlers := NewHandlers(nil, nil, nil, "test-booking-secret")
	office, _ := domain.LookupOffice("Hollywood")
	if err := handlers.applyBookingToken(&req, office, now); err == nil {
		t.Fatal("token for a different office should be rejected")
	}
}

func TestAddBookingTokensAndApplyBookingToken(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	handlers := NewHandlers(nil, nil, nil, "test-booking-secret")
	office := domain.DefaultOffice()
	slots := []domain.AvailabilitySlotOption{
		{
			Provider:  "Dr. Austin Bach",
			Time:      "9:00 AM",
			DateTime:  "2026-06-02T09:00",
			ColumnID:  1513,
			ProfileID: 620,
			Duration:  15,
		},
	}

	slots, err := handlers.addBookingTokens(slots, office, domain.RoutingBachOnly, now)
	if err != nil {
		t.Fatalf("addBookingTokens error = %v", err)
	}
	if slots[0].BookingToken == "" {
		t.Fatal("expected booking token")
	}

	req := BookAppointmentRequest{BookingToken: slots[0].BookingToken}
	if err := handlers.applyBookingToken(&req, office, now); err != nil {
		t.Fatalf("applyBookingToken error = %v", err)
	}
	if req.ColumnID != 1513 ||
		req.ProfileID != 620 ||
		req.StartDatetime != "2026-06-02T09:00" ||
		req.Duration != 15 ||
		req.Routing != string(domain.RoutingBachOnly) {
		t.Fatalf("request populated from token = %+v", req)
	}
}

func TestHasDifferentStartOverlappingAppointment(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")

	tests := []struct {
		name         string
		slotTime     time.Time
		slotDuration time.Duration
		appointments []domain.Appointment
		expected     bool
	}{
		{
			name:         "no appointments",
			slotTime:     time.Date(2026, 3, 6, 9, 30, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: nil,
			expected:     false,
		},
		{
			name:         "30-min appt ends exactly at slot — no overlap",
			slotTime:     time.Date(2026, 3, 6, 9, 30, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 30},
			},
			expected: false, // 9:00+30min=9:30, [9:30,10:00) does not overlap [9:00,9:30)
		},
		{
			name:         "60-min appt overlaps into next slot — blocked (4101)",
			slotTime:     time.Date(2026, 3, 6, 9, 30, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 60},
			},
			expected: true, // [9:30,10:00) overlaps [9:00,10:00)
		},
		{
			name:         "60-min appt does not overlap past its end",
			slotTime:     time.Date(2026, 3, 6, 10, 0, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 60},
			},
			expected: false, // [10:00,10:30) does not overlap [9:00,10:00)
		},
		{
			name:         "same-start-time appt is capacity, not hard overlap",
			slotTime:     time.Date(2026, 3, 6, 9, 0, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 30},
			},
			expected: false, // same-start capacity is handled separately from AMD 4101 overlap
		},
		{
			name:         "Licht 12:15 scenario — Bourque at 12:00 with 30-min duration blocks 12:15",
			slotTime:     time.Date(2026, 3, 10, 12, 15, 0, 0, eastern),
			slotDuration: 15 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 10, 12, 0, 0, 0, eastern), Duration: 30}, // Bourque 12:00-12:30
			},
			expected: true, // [12:15,12:30) overlaps [12:00,12:30) — AMD 4101
		},
		{
			name:         "overlap from earlier appt even with same-start appt present",
			slotTime:     time.Date(2026, 3, 6, 9, 30, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 60},  // overlaps into 9:30
				{StartDateTime: time.Date(2026, 3, 6, 9, 30, 0, 0, eastern), Duration: 30}, // starts at 9:30
			},
			expected: true, // the 9:00 appt overlaps — hard block regardless of the 9:30 same-start
		},
		{
			name:         "off-grid appt at 8:45 blocks 30-min booking at 8:30",
			slotTime:     time.Date(2026, 5, 13, 8, 30, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 5, 13, 8, 45, 0, 0, eastern), Duration: 15},
			},
			expected: true, // [8:30,9:00) overlaps [8:45,9:00) — the bug this fix addresses
		},
		{
			name:         "off-grid appt at 9:15 blocks 30-min booking at 9:00",
			slotTime:     time.Date(2026, 5, 13, 9, 0, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 5, 13, 9, 15, 0, 0, eastern), Duration: 15},
			},
			expected: true, // [9:00,9:30) overlaps [9:15,9:30)
		},
		{
			name:         "off-grid appt at 8:45 does NOT block 8:00 slot",
			slotTime:     time.Date(2026, 5, 13, 8, 0, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 5, 13, 8, 45, 0, 0, eastern), Duration: 15},
			},
			expected: false, // [8:00,8:30) does not overlap [8:45,9:00)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasDifferentStartOverlappingAppointment(tt.slotTime, tt.slotDuration, tt.appointments)
			if got != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestCountSameStartAppointments(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")

	tests := []struct {
		name         string
		slotTime     time.Time
		appointments []domain.Appointment
		expected     int
	}{
		{
			name:         "no appointments",
			slotTime:     time.Date(2026, 3, 6, 9, 0, 0, 0, eastern),
			appointments: nil,
			expected:     0,
		},
		{
			name:     "one same-start appointment",
			slotTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern),
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 15},
			},
			expected: 1,
		},
		{
			name:     "two same-start appointments (double-book)",
			slotTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern),
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 15},
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 15},
			},
			expected: 2,
		},
		{
			name:     "different-start appointments not counted",
			slotTime: time.Date(2026, 3, 6, 9, 30, 0, 0, eastern),
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 60}, // overlaps but different start
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countSameStartAppointments(tt.slotTime, tt.appointments)
			if got != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestForceForBachBooking(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	office := domain.DefaultOffice()
	slotTime := time.Date(2026, 6, 1, 9, 0, 0, 0, eastern)
	slotDuration := 15 * time.Minute

	tests := []struct {
		name         string
		columnID     string
		appointments []domain.Appointment
		blockHolds   []domain.BlockHold
		wantForce    int
		wantErr      bool
	}{
		{
			name:      "Bach empty slot books normally",
			columnID:  "1513",
			wantForce: 0,
		},
		{
			name:     "Bach one same-start appointment requires force",
			columnID: "1513",
			appointments: []domain.Appointment{
				{StartDateTime: slotTime, Duration: 15},
			},
			wantForce: 1,
		},
		{
			name:     "Bach full same-start capacity errors",
			columnID: "1513",
			appointments: []domain.Appointment{
				{StartDateTime: slotTime, Duration: 15},
				{StartDateTime: slotTime, Duration: 15},
			},
			wantErr: true,
		},
		{
			name:     "Bach different-start overlap errors",
			columnID: "1513",
			appointments: []domain.Appointment{
				{StartDateTime: slotTime.Add(-15 * time.Minute), Duration: 30},
			},
			wantErr: true,
		},
		{
			name:     "Bach block hold errors",
			columnID: "1513",
			blockHolds: []domain.BlockHold{
				{StartDateTime: slotTime, EndDateTime: slotTime.Add(15 * time.Minute)},
			},
			wantErr: true,
		},
		{
			name:     "non-Bach column does not use force",
			columnID: "1551",
			appointments: []domain.Appointment{
				{StartDateTime: slotTime, Duration: 15},
			},
			wantForce: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotForce, err := forceForBachBooking(office, tt.columnID, slotTime, slotDuration, tt.appointments, tt.blockHolds)
			if (err != nil) != tt.wantErr {
				t.Fatalf("forceForBachBooking error = %v, wantErr %v", err, tt.wantErr)
			}
			if gotForce != tt.wantForce {
				t.Fatalf("force = %d, want %d", gotForce, tt.wantForce)
			}
		})
	}
}

func TestBachBookingLock(t *testing.T) {
	first := bachBookingLock("spring_hill", "1513", "2026-06-01T09:00")
	second := bachBookingLock("spring_hill", "1513", "2026-06-01T09:00")
	otherColumn := bachBookingLock("spring_hill", "1598", "2026-06-01T09:00")
	otherStart := bachBookingLock("spring_hill", "1513", "2026-06-01T09:15")

	if first != second {
		t.Fatal("same Bach office/column/start should reuse one lock")
	}
	if first == otherColumn {
		t.Fatal("different Bach columns should use distinct locks")
	}
	if first == otherStart {
		t.Fatal("different Bach start times should use distinct locks")
	}
}

func TestCalculateAvailableSlots_MultiSlotAppointment(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	// Use a future Friday so it's not "today"
	date := time.Date(2026, 3, 6, 0, 0, 0, 0, eastern)
	nowEastern := time.Date(2026, 3, 5, 10, 0, 0, 0, eastern) // day before

	// Dr. Noel: 30-min intervals, max 2 per slot, 8:30-16:30
	col := domain.SchedulerColumn{
		ID:              "1550",
		Name:            "DR. NOEL",
		StartTime:       "08:30",
		EndTime:         "10:30",
		Interval:        30,
		MaxApptsPerSlot: 2,
		Workweek:        62, // Mon-Fri
	}

	// Simulate: 60-min appt at 9:00 (Vargas) + 30-min appt at 9:30 (Prater)
	// 9:30 is hard-blocked by Vargas overlap (AMD 4101), regardless of maxAppts
	appointments := []domain.Appointment{
		{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 60},  // Vargas 9:00-10:00
		{StartDateTime: time.Date(2026, 3, 6, 9, 30, 0, 0, eastern), Duration: 30}, // Prater 9:30-10:00
	}

	// Block hold at 8:30 (OUT OF THE OFFICE)
	blockHolds := []domain.BlockHold{
		{
			StartDateTime: time.Date(2026, 3, 6, 8, 30, 0, 0, eastern),
			EndDateTime:   time.Date(2026, 3, 6, 9, 0, 0, 0, eastern),
			Note:          "OUT OF THE OFFICE",
		},
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, appointments, blockHolds, date, nowEastern)

	// 8:30 — blocked by hold
	// 9:00 — one same-start appt and max=2 → available
	// 9:30 — Vargas (9:00, 60min) overlaps into 9:30 → blocked
	// 10:00 — 0 appts → available

	if len(slots) != 2 {
		t.Errorf("Expected 2 available slots, got %d: %v", len(slots), slots)
		return
	}

	if slots[0].Time != "9:00 AM" {
		t.Errorf("Expected 9:00 AM, got %s", slots[0].Time)
	}
	if slots[0].SameStartBooked != 1 || slots[0].SameStartCapacity != 2 {
		t.Errorf("Expected 9:00 AM same-start metadata 1/2, got %d/%d", slots[0].SameStartBooked, slots[0].SameStartCapacity)
	}
	if slots[0].RequiresForce {
		t.Error("Expected non-Bach same-start slot not to require force")
	}
	if slots[1].Time != "10:00 AM" {
		t.Errorf("Expected 10:00 AM, got %s", slots[1].Time)
	}
}

func TestCalculateAvailableSlots_BachSingleBookedSlotsAvailableWithForceMetadata(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, eastern) // Monday
	nowEastern := time.Date(2026, 5, 31, 10, 0, 0, 0, eastern)

	// Dr. Bach uses explicit middleware capacity even when AMD reports max=0.
	col := domain.SchedulerColumn{
		ID:              "1513",
		Name:            "DR. BACH - BP",
		StartTime:       "09:00",
		EndTime:         "09:30",
		Interval:        15,
		MaxApptsPerSlot: 0,
		Workweek:        62,
	}

	appointments := []domain.Appointment{
		{StartDateTime: time.Date(2026, 6, 1, 9, 0, 0, 0, eastern), Duration: 15},
		{StartDateTime: time.Date(2026, 6, 1, 9, 15, 0, 0, eastern), Duration: 15},
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, appointments, nil, date, nowEastern)

	if len(slots) != 2 {
		t.Fatalf("Expected 2 second-bookable Bach slots, got %d: %v", len(slots), slots)
	}
	for _, slot := range slots {
		if slot.SameStartBooked != 1 {
			t.Errorf("slot %s SameStartBooked = %d, want 1", slot.Time, slot.SameStartBooked)
		}
		if slot.SameStartCapacity != bachSameStartCapacity {
			t.Errorf("slot %s SameStartCapacity = %d, want %d", slot.Time, slot.SameStartCapacity, bachSameStartCapacity)
		}
		if !slot.RequiresForce {
			t.Errorf("slot %s should require force", slot.Time)
		}
	}
}

func TestCalculateAvailableSlots_BachTwoSameStartAppointmentsBlockSlot(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, eastern) // Monday
	nowEastern := time.Date(2026, 5, 31, 10, 0, 0, 0, eastern)

	col := domain.SchedulerColumn{
		ID:              "1513",
		Name:            "DR. BACH - BP",
		StartTime:       "09:00",
		EndTime:         "09:15",
		Interval:        15,
		MaxApptsPerSlot: 0,
		Workweek:        62,
	}

	appointments := []domain.Appointment{
		{StartDateTime: time.Date(2026, 6, 1, 9, 0, 0, 0, eastern), Duration: 15},
		{StartDateTime: time.Date(2026, 6, 1, 9, 0, 0, 0, eastern), Duration: 15},
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, appointments, nil, date, nowEastern)
	if len(slots) != 0 {
		t.Fatalf("Expected no slots when Bach same-start capacity is full, got %d: %v", len(slots), slots)
	}
}

func TestCalculateAvailableSlots_NonBachMaxZeroBlocksSameStart(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, eastern) // Monday
	nowEastern := time.Date(2026, 5, 31, 10, 0, 0, 0, eastern)

	col := domain.SchedulerColumn{
		ID:              "1600",
		Name:            "ROUTINE VISION",
		StartTime:       "09:00",
		EndTime:         "09:15",
		Interval:        15,
		MaxApptsPerSlot: 0,
		Workweek:        62,
	}
	appointments := []domain.Appointment{
		{StartDateTime: time.Date(2026, 6, 1, 9, 0, 0, 0, eastern), Duration: 15},
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, appointments, nil, date, nowEastern)
	if len(slots) != 0 {
		t.Fatalf("Expected non-Bach max=0 same-start slot to be blocked, got %d: %v", len(slots), slots)
	}
}

func TestCalculateAvailableSlots_MaxTwoSingleSameStartAvailable(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, eastern) // Monday
	nowEastern := time.Date(2026, 5, 31, 10, 0, 0, 0, eastern)

	col := domain.SchedulerColumn{
		ID:              "1551",
		Name:            "DR. LICHT",
		StartTime:       "09:00",
		EndTime:         "09:15",
		Interval:        15,
		MaxApptsPerSlot: 2,
		Workweek:        62,
	}
	appointments := []domain.Appointment{
		{StartDateTime: time.Date(2026, 6, 1, 9, 0, 0, 0, eastern), Duration: 15},
	}

	slots := calculateAvailableSlots(domain.DefaultOffice(), col, appointments, nil, date, nowEastern)
	if len(slots) != 1 {
		t.Fatalf("Expected one same-start slot below max capacity, got %d: %v", len(slots), slots)
	}
	if slots[0].SameStartBooked != 1 || slots[0].SameStartCapacity != 2 {
		t.Fatalf("slot metadata = booked %d capacity %d, want 1/2", slots[0].SameStartBooked, slots[0].SameStartCapacity)
	}
	if slots[0].RequiresForce {
		t.Fatal("non-Bach max-capacity slot should not require AMD force")
	}
}

func TestEnforcePreauthMinDate(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, eastern) // March 10, 2026

	tests := []struct {
		name          string
		requestDate   time.Time
		expectedDate  string
		shouldAdvance bool
	}{
		{
			name:          "date tomorrow — advances to 14 days out",
			requestDate:   time.Date(2026, 3, 11, 0, 0, 0, 0, eastern),
			expectedDate:  "2026-03-24",
			shouldAdvance: true,
		},
		{
			name:          "date 7 days out — advances to 14 days out",
			requestDate:   time.Date(2026, 3, 17, 0, 0, 0, 0, eastern),
			expectedDate:  "2026-03-24",
			shouldAdvance: true,
		},
		{
			name:          "date 13 days out — still advances to 14 days out",
			requestDate:   time.Date(2026, 3, 23, 0, 0, 0, 0, eastern),
			expectedDate:  "2026-03-24",
			shouldAdvance: true,
		},
		{
			name:          "date exactly 14 days out — no change",
			requestDate:   time.Date(2026, 3, 24, 0, 0, 0, 0, eastern),
			expectedDate:  "2026-03-24",
			shouldAdvance: false,
		},
		{
			name:          "date 30 days out — no change",
			requestDate:   time.Date(2026, 4, 9, 0, 0, 0, 0, eastern),
			expectedDate:  "2026-04-09",
			shouldAdvance: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultDate, resultStr := enforcePreauthMinDate(tt.requestDate, now)
			if resultStr != tt.expectedDate {
				t.Errorf("Expected date %s, got %s", tt.expectedDate, resultStr)
			}
			if tt.shouldAdvance && resultDate.Equal(tt.requestDate) {
				t.Error("Expected date to be advanced but it wasn't")
			}
			if !tt.shouldAdvance && !resultDate.Equal(tt.requestDate) {
				t.Errorf("Expected date to stay the same but it changed to %s", resultStr)
			}
		})
	}
}

func TestHandleGetPatientAppointments_ValidationErrors(t *testing.T) {
	handlers := &Handlers{}

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
			body:        `{}`,
			expectedMsg: "patientId is required",
		},
		{
			name:        "empty patientId",
			body:        `{"patientId":""}`,
			expectedMsg: "patientId is required",
		},
		{
			name:        "non-numeric patientId",
			body:        `{"patientId":"abc"}`,
			expectedMsg: "patientId must be numeric",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/patient/appointments", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleGetPatientAppointments(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var body PatientApptResponse
			json.NewDecoder(resp.Body).Decode(&body)
			if body.Status != "error" {
				t.Errorf("Expected status 'error', got '%s'", body.Status)
			}
			if body.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, body.Message)
			}
		})
	}
}

func TestFriendlyProviderName(t *testing.T) {
	office := domain.DefaultOffice()

	tests := []struct {
		input    string
		expected string
	}{
		{"BACH, AUSTIN", "Dr. Austin Bach"},
		{"NOEL, DON HERSHELSON", "Dr. D. Noel"},
		{"LICHT, JONATHAN", "Dr. J. Licht"},
		{"UNKNOWN PROVIDER", "UNKNOWN PROVIDER"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := office.FriendlyProviderName(tt.input)
			if got != tt.expected {
				t.Errorf("FriendlyProviderName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFriendlyFacilityName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABITA EYE GROUP SPRING HILL", "Abita Eye Group Spring Hill"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := friendlyFacilityName(tt.input)
			if got != tt.expected {
				t.Errorf("friendlyFacilityName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestAppointmentTypeNames(t *testing.T) {
	office := domain.DefaultOffice()

	tests := []struct {
		typeID   int
		expected string
		found    bool
	}{
		{1006, "New Adult Medical", true},
		{1004, "New Pediatric Medical", true},
		{1007, "Established Adult Medical (Follow Up)", true},
		{1005, "Established Pediatric Medical (Follow Up)", true},
		{1008, "Post Op", true},
		{1010, "New Adult Vision", true},
		{3364, "Established Adult Vision", true},
		{4244, "New Pediatric Vision", true},
		{4245, "Established Pediatric Vision", true},
		{6167, "Crystal River New Patient", true},
		{6168, "Crystal River Post Op", true},
		{6169, "Crystal River Established Patient", true},
		{9999, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got, ok := office.AppointmentTypeName(tt.typeID)
			if ok != tt.found {
				t.Errorf("AppointmentTypeName(%d) found=%v, want %v", tt.typeID, ok, tt.found)
			}
			if got != tt.expected {
				t.Errorf("AppointmentTypeName(%d) = %q, want %q", tt.typeID, got, tt.expected)
			}
		})
	}
}

func TestRouter(t *testing.T) {
	// Create minimal handlers for testing
	amdClient := clients.NewAdvancedMDClient(&http.Client{})
	handlers := NewHandlers(nil, amdClient, nil) // nil token manager - can't test full flow

	router := NewRouter(handlers, "test-secret")

	t.Run("health endpoint no auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}
	})

	t.Run("api endpoints require auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/token", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 without auth, got %d", w.Code)
		}
	})

	t.Run("api endpoints with auth", func(t *testing.T) {
		// Skip this test - it requires a real token manager
		// The important thing is that auth middleware works (tested above)
		t.Skip("Requires non-nil token manager")
	})
}

func TestHandleCancelAppointment_ValidationErrors(t *testing.T) {
	handlers := &Handlers{}

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
			name:        "missing appointmentId",
			body:        `{}`,
			expectedMsg: "appointmentId is required",
		},
		{
			name:        "zero appointmentId",
			body:        `{"appointmentId":0}`,
			expectedMsg: "appointmentId is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/appointment/cancel", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleCancelAppointment(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}

			var body CancelAppointmentResponse
			json.NewDecoder(resp.Body).Decode(&body)
			if body.Status != "error" {
				t.Errorf("Expected status 'error', got '%s'", body.Status)
			}
			if body.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, body.Message)
			}
		})
	}
}

func TestPatientApptDetail_IncludesID(t *testing.T) {
	detail := PatientApptDetail{
		ID:        9570263,
		Date:      "Wednesday, March 18, 2026",
		Time:      "12:00 PM",
		Provider:  "Dr. Austin Bach",
		Type:      "New Adult Medical",
		Facility:  "Abita Eye Group Spring Hill",
		Confirmed: false,
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
}

func TestHandleUpdateInsurance_ValidationErrors(t *testing.T) {
	handlers := &Handlers{}

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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

	authenticator := auth.NewAuthenticator(auth.Credentials{
		Username:  "user",
		Password:  "pass",
		OfficeKey: "office",
		AppName:   "app",
	}, httpClient)
	tokenManager := auth.NewTokenManager(authenticator)

	return NewHandlers(
		tokenManager,
		clients.NewAdvancedMDClient(httpClient),
		clients.NewAdvancedMDRestClient(httpClient),
	), &writes
}

func TestHandleAddPatientNote_ValidationErrors(t *testing.T) {
	handlers := &Handlers{}
	longNote := strings.Repeat("x", maxPatientNoteLength+1)

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
			body:        `{"note":"Patient called to reschedule."}`,
			expectedMsg: "patientId is required",
		},
		{
			name:        "missing note",
			body:        `{"patientId":"123"}`,
			expectedMsg: "note is required",
		},
		{
			name:        "non-numeric patientId",
			body:        `{"patientId":"abc123","note":"Patient called."}`,
			expectedMsg: "patientId must be numeric",
		},
		{
			name:        "blank note",
			body:        `{"patientId":"123","note":"   "}`,
			expectedMsg: "note is required",
		},
		{
			name:        "note too long",
			body:        fmt.Sprintf(`{"patientId":"123","note":%q}`, longNote),
			expectedMsg: "note must be 1000 characters or fewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/patient/notes", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleAddPatientNote(w, req)

			var body AddPatientNoteResponse
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

func TestSanitizeLoggedRequestBody_RedactsPatientNote(t *testing.T) {
	body := `{"patientId":"123","note":"Patient shared private details.","office":"+17275919997"}`

	got := sanitizeLoggedRequestBody("/api/patient/notes", body)

	if strings.Contains(got, "Patient shared private details") {
		t.Fatalf("expected patient note to be redacted, got %s", got)
	}
	if !strings.Contains(got, `"[REDACTED]"`) {
		t.Fatalf("expected redaction marker, got %s", got)
	}
	if !strings.Contains(got, `"patientId":"123"`) {
		t.Fatalf("expected non-note fields to remain, got %s", got)
	}
}

func TestSanitizeLoggedRequestBody_LeavesOtherRoutesUnchanged(t *testing.T) {
	body := `{"note":"not a patient note route"}`

	got := sanitizeLoggedRequestBody("/api/add-patient", body)

	if got != body {
		t.Fatalf("expected body unchanged, got %s", got)
	}
}
