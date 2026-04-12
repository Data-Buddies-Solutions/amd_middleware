package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestHandleVerifyPatient_ValidationErrors(t *testing.T) {
	handlers := &Handlers{}

	tests := []struct {
		name            string
		method          string
		body            string
		expectedMsg     string
		expectedOutcome string
	}{
		{
			name:            "invalid JSON",
			method:          "POST",
			body:            "not json",
			expectedMsg:     "Invalid JSON body.",
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing lastName and phone",
			method:          "POST",
			body:            `{"dob":"01/15/1980"}`,
			expectedMsg:     "Provide phone plus first name, phone plus date of birth, or last name plus date of birth.",
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing dob",
			method:          "POST",
			body:            `{"lastName":"Smith"}`,
			expectedMsg:     "Provide phone plus first name, phone plus date of birth, or last name plus date of birth.",
			expectedOutcome: "invalid_request",
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

			var body ToolResponse[VerifyPatientStructured]
			json.NewDecoder(resp.Body).Decode(&body)
			if !body.IsError {
				t.Errorf("Expected isError=true, got false")
			}
			if len(body.Content) != 1 || body.Content[0].Text != tt.expectedMsg {
				t.Errorf("Expected message '%s', got %+v", tt.expectedMsg, body.Content)
			}
			if body.StructuredContent.Outcome != tt.expectedOutcome {
				t.Errorf("Expected outcome %q, got %q", tt.expectedOutcome, body.StructuredContent.Outcome)
			}
		})
	}
}

func TestBookingTokenRoundTrip(t *testing.T) {
	handlers := &Handlers{bookingTokenSecret: "test-secret"}
	token, err := handlers.mintBookingToken(bookingTokenPayload{
		ColumnID:      101,
		ProfileID:     202,
		FacilityID:    "303",
		StartDatetime: "2026-04-24T09:00",
		Duration:      15,
		Office:        domain.DefaultPhone,
	})
	if err != nil {
		t.Fatalf("mintBookingToken failed: %v", err)
	}

	payload, err := handlers.parseBookingToken(token)
	if err != nil {
		t.Fatalf("parseBookingToken failed: %v", err)
	}
	if payload.ColumnID != 101 || payload.ProfileID != 202 || payload.FacilityID != "303" ||
		payload.StartDatetime != "2026-04-24T09:00" || payload.Duration != 15 || payload.Office != domain.DefaultPhone {
		t.Fatalf("unexpected parsed payload: %+v", payload)
	}
}

func TestBookingTokenRejectsExpiredAndTamperedTokens(t *testing.T) {
	handlers := &Handlers{bookingTokenSecret: "test-secret"}

	expiredToken, err := handlers.mintBookingToken(bookingTokenPayload{
		ColumnID:      101,
		ProfileID:     202,
		FacilityID:    "303",
		StartDatetime: "2026-04-24T09:00",
		Duration:      15,
		Office:        domain.DefaultPhone,
		Exp:           time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("mintBookingToken failed: %v", err)
	}
	if _, err := handlers.parseBookingToken(expiredToken); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired token error, got %v", err)
	}

	validToken, err := handlers.mintBookingToken(bookingTokenPayload{
		ColumnID:      101,
		ProfileID:     202,
		FacilityID:    "303",
		StartDatetime: "2026-04-24T09:00",
		Duration:      15,
		Office:        domain.DefaultPhone,
	})
	if err != nil {
		t.Fatalf("mintBookingToken failed: %v", err)
	}
	tamperedToken := validToken[:len(validToken)-1] + "x"
	if _, err := handlers.parseBookingToken(tamperedToken); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected invalid signature error, got %v", err)
	}
}

func TestHandleBookAppointment_InvalidBookingToken(t *testing.T) {
	handlers := &Handlers{bookingTokenSecret: "test-secret"}

	req := httptest.NewRequest("POST", "/api/appointment/book", bytes.NewBufferString(`{
		"patientId":"12345",
		"bookingToken":"bad.token",
		"appointmentTypeId":1007
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.HandleBookAppointment(w, req)

	var body ToolResponse[BookAppointmentStructured]
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.IsError {
		t.Fatalf("expected isError=true, got false")
	}
	if body.StructuredContent.Outcome != "stale_booking_token" {
		t.Fatalf("expected outcome stale_booking_token, got %q", body.StructuredContent.Outcome)
	}
}

func TestPreauthRequiredForCarrierID(t *testing.T) {
	tests := []struct {
		carrierID string
		expected  bool
	}{
		{carrierID: "car40897", expected: false},  // Florida Blue mixes PPO and HMO
		{carrierID: "car40907", expected: false},  // Aetna iCare mix
		{carrierID: "car40921", expected: false},  // Tricare mixed
		{carrierID: "car301737", expected: false}, // not accepted only
		{carrierID: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.carrierID, func(t *testing.T) {
			if got := preauthRequiredForCarrierID(tt.carrierID); got != tt.expected {
				t.Fatalf("preauthRequiredForCarrierID(%q) = %v, want %v", tt.carrierID, got, tt.expected)
			}
		})
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

	slots := calculateAvailableSlots(col, nil, blockHolds, date, nowEastern)

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

	slots := calculateAvailableSlots(col, appointments, nil, date, nowEastern)

	if len(slots) != 0 {
		t.Errorf("Expected 0 slots when all slots at max capacity, got %d", len(slots))
	}
}

func TestNoAvailabilityResponse_HasMessageAndEmptyProviders(t *testing.T) {
	resp := domain.AvailabilityResponse{
		SearchedDate: "2026-05-15",
		Date:         "",
		Location:     "ABITA EYE GROUP SPRING HILL",
		Message:      "No availability found within 14 days of requested date",
		Providers:    []domain.ProviderAvailability{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if decoded["date"] != "" {
		t.Errorf("Expected empty date, got %q", decoded["date"])
	}
	if decoded["message"] != "No availability found within 14 days of requested date" {
		t.Errorf("Expected no-availability message, got %q", decoded["message"])
	}
	providers := decoded["providers"].([]interface{})
	if len(providers) != 0 {
		t.Errorf("Expected empty providers array, got %d", len(providers))
	}
}

func TestAvailabilityResponse_OmitsMessageWhenEmpty(t *testing.T) {
	resp := domain.AvailabilityResponse{
		SearchedDate: "2026-05-15",
		Date:         "Monday, June 1, 2026",
		Location:     "ABITA EYE GROUP SPRING HILL",
		Providers:    []domain.ProviderAvailability{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)

	if _, exists := decoded["message"]; exists {
		t.Error("Expected message field to be omitted when empty")
	}
}

func TestHasOverlappingAppointment(t *testing.T) {
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
			name:         "same-start-time appt is blocked — no double booking",
			slotTime:     time.Date(2026, 3, 6, 9, 0, 0, 0, eastern),
			slotDuration: 30 * time.Minute,
			appointments: []domain.Appointment{
				{StartDateTime: time.Date(2026, 3, 6, 9, 0, 0, 0, eastern), Duration: 30},
			},
			expected: true, // [9:00,9:30) overlaps [9:00,9:30)
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
			got := hasOverlappingAppointment(tt.slotTime, tt.slotDuration, tt.appointments)
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

	slots := calculateAvailableSlots(col, appointments, blockHolds, date, nowEastern)

	// 8:30 — blocked by hold
	// 9:00 — Vargas appt here (60min) → blocked
	// 9:30 — Vargas (9:00, 60min) overlaps into 9:30 → blocked
	// 10:00 — 0 appts → available

	if len(slots) != 1 {
		t.Errorf("Expected 1 available slot, got %d: %v", len(slots), slots)
		return
	}

	if slots[0].Time != "10:00 AM" {
		t.Errorf("Expected 10:00 AM, got %s", slots[0].Time)
	}
}

func TestCalculateAvailableSlots_UnlimitedMaxAppts(t *testing.T) {
	eastern, _ := time.LoadLocation("America/New_York")
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, eastern) // Monday
	nowEastern := time.Date(2026, 5, 31, 10, 0, 0, 0, eastern)

	// Dr. Bach: max=0 (unlimited), 15-min intervals
	col := domain.SchedulerColumn{
		ID:              "1513",
		Name:            "DR. BACH - BP",
		StartTime:       "09:00",
		EndTime:         "09:30",
		Interval:        15,
		MaxApptsPerSlot: 0, // unlimited
		Workweek:        62,
	}

	// All slots occupied — none should be available regardless of unlimited maxAppts
	appointments := []domain.Appointment{
		{StartDateTime: time.Date(2026, 6, 1, 9, 0, 0, 0, eastern), Duration: 15},
		{StartDateTime: time.Date(2026, 6, 1, 9, 15, 0, 0, eastern), Duration: 15},
	}

	slots := calculateAvailableSlots(col, appointments, nil, date, nowEastern)

	// Both 9:00 and 9:15 are occupied — no slots available
	if len(slots) != 0 {
		t.Errorf("Expected 0 slots when all occupied, got %d", len(slots))
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
		name            string
		body            string
		expectedMsg     string
		expectedIsError bool
		expectedOutcome string
	}{
		{
			name:            "invalid JSON",
			body:            "not json",
			expectedMsg:     "Invalid JSON body.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing patientId",
			body:            `{}`,
			expectedMsg:     "patientId is required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "empty patientId",
			body:            `{"patientId":""}`,
			expectedMsg:     "patientId is required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "non-numeric patientId",
			body:            `{"patientId":"abc"}`,
			expectedMsg:     "patientId must be numeric.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
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

			var body ToolResponse[PatientAppointmentsStructured]
			json.NewDecoder(resp.Body).Decode(&body)
			if body.IsError != tt.expectedIsError {
				t.Errorf("Expected isError %v, got %v", tt.expectedIsError, body.IsError)
			}
			if got := body.Content[0].Text; got != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, got)
			}
			if body.StructuredContent.Outcome != tt.expectedOutcome {
				t.Errorf("Expected outcome %q, got %q", tt.expectedOutcome, body.StructuredContent.Outcome)
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
	handlers := NewHandlers(nil, amdClient, nil, "test-secret") // nil token manager - can't test full flow

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
		name            string
		body            string
		expectedMsg     string
		expectedIsError bool
		expectedOutcome string
	}{
		{
			name:            "invalid JSON",
			body:            "not json",
			expectedMsg:     "Invalid JSON body.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing appointmentId",
			body:            `{}`,
			expectedMsg:     "appointmentId is required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "zero appointmentId",
			body:            `{"appointmentId":0}`,
			expectedMsg:     "appointmentId is required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
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

			var body ToolResponse[CancelAppointmentStructured]
			json.NewDecoder(resp.Body).Decode(&body)
			if body.IsError != tt.expectedIsError {
				t.Errorf("Expected isError %v, got %v", tt.expectedIsError, body.IsError)
			}
			if got := body.Content[0].Text; got != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, got)
			}
			if body.StructuredContent.Outcome != tt.expectedOutcome {
				t.Errorf("Expected outcome %q, got %q", tt.expectedOutcome, body.StructuredContent.Outcome)
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
		name            string
		body            string
		expectedMsg     string
		expectedIsError bool
		expectedOutcome string
	}{
		{
			name:            "invalid JSON",
			body:            "not json",
			expectedMsg:     "Invalid JSON body.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing patientId",
			body:            `{"insurance":"Aetna","subscriberNum":"ABC123"}`,
			expectedMsg:     "patientId, insurance, and subscriberNum are required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing insurance",
			body:            `{"patientId":"pat123","subscriberNum":"ABC123"}`,
			expectedMsg:     "patientId, insurance, and subscriberNum are required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "missing subscriberNum",
			body:            `{"patientId":"pat123","insurance":"Aetna"}`,
			expectedMsg:     "patientId, insurance, and subscriberNum are required.",
			expectedIsError: true,
			expectedOutcome: "invalid_request",
		},
		{
			name:            "insurance not recognized",
			body:            `{"patientId":"pat123","insurance":"FakeInsurance","subscriberNum":"ABC123"}`,
			expectedMsg:     `Insurance not recognized: "FakeInsurance". Please use an insurance name from the accepted list.`,
			expectedIsError: false,
			expectedOutcome: "unrecognized_insurance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/patient/update-insurance", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.HandleUpdateInsurance(w, req)

			var body ToolResponse[UpdateInsuranceStructured]
			json.NewDecoder(w.Result().Body).Decode(&body)
			if body.IsError != tt.expectedIsError {
				t.Errorf("Expected isError %v, got %v", tt.expectedIsError, body.IsError)
			}
			if got := body.Content[0].Text; got != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, got)
			}
			if body.StructuredContent.Outcome != tt.expectedOutcome {
				t.Errorf("Expected outcome %q, got %q", tt.expectedOutcome, body.StructuredContent.Outcome)
			}
		})
	}
}
