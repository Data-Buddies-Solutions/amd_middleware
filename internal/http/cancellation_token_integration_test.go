package http

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/advancedmd"
	"advancedmd-token-management/internal/advancedmd/advancedmdtest"
	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/patient"
	"advancedmd-token-management/internal/safeerrors"
	"advancedmd-token-management/internal/scheduling"
)

func TestCancellationTokenCancelsPairedOfficeAppointmentWithoutRediscovery(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{{
				ID:       33333,
				Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
				OfficeID: "crystal_river",
				Office:   "Crystal River",
			}},
			Complete: true,
		},
	}
	tokens := scheduling.NewAppointmentTokens("test-scheduling-secret", func() time.Time { return now })
	handlers := NewHandlers(
		nil,
		patient.NewWithAppointmentTokens(records, tokens),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)

	resolveRecorder := postJSON(t, handlers.HandlePatientResolve, "/api/patient/resolve", map[string]any{
		"patientId": "12345",
		"office":    "Spring Hill",
	})
	var resolved struct {
		Status       string `json:"status"`
		Appointments []struct {
			ID                int    `json:"id"`
			OfficeID          string `json:"officeId"`
			CancellationToken string `json:"cancellationToken"`
			RescheduleToken   string `json:"rescheduleToken"`
		} `json:"appointments"`
	}
	if err := json.NewDecoder(resolveRecorder.Body).Decode(&resolved); err != nil {
		t.Fatalf("decode patient resolution: %v", err)
	}
	if resolved.Status != "verified" || len(resolved.Appointments) != 1 {
		t.Fatalf("patient resolution = %#v", resolved)
	}
	appointment := resolved.Appointments[0]
	if appointment.ID != 33333 ||
		appointment.OfficeID != "crystal_river" ||
		appointment.CancellationToken == "" ||
		appointment.RescheduleToken == "" ||
		appointment.RescheduleToken == appointment.CancellationToken {
		t.Fatalf("resolved appointment = %#v", appointment)
	}
	if records.appointmentReads != 1 {
		t.Fatalf("appointment reads after resolution = %d, want 1", records.appointmentReads)
	}

	cancelRecorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", map[string]any{
		"patientId":         "12345",
		"appointmentId":     33333,
		"office":            "Crystal River",
		"cancellationToken": appointment.CancellationToken,
	})
	var cancelled CancelAppointmentResponse
	if err := json.NewDecoder(cancelRecorder.Body).Decode(&cancelled); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.AppointmentID != 33333 {
		t.Fatalf("cancellation response = %#v", cancelled)
	}
	if records.appointmentReads != 1 {
		t.Fatalf("appointment reads after cancellation = %d, want no rediscovery", records.appointmentReads)
	}
	if len(records.Cancellations) != 1 {
		t.Fatalf("provider cancellations = %#v, want exactly one", records.Cancellations)
	}
	cancellation := records.Cancellations[0]
	if cancellation.PatientID != "12345" ||
		cancellation.AppointmentID != 33333 ||
		cancellation.OfficeID != "crystal_river" {
		t.Fatalf("provider cancellation = %#v", cancellation)
	}
}

func TestPatientResolutionIssuesOneDistinctTokenPerAppointment(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{
				{
					ID:       33333,
					Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
					OfficeID: "spring_hill",
					Office:   "Spring Hill",
				},
				{
					ID:       44444,
					Start:    time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
					OfficeID: "crystal_river",
					Office:   "Crystal River",
				},
			},
			Complete: true,
		},
	}
	tokens := scheduling.NewAppointmentTokens("test-scheduling-secret", func() time.Time { return now })
	handlers := NewHandlers(
		nil,
		patient.NewWithAppointmentTokens(records, tokens),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)

	resolveRecorder := postJSON(t, handlers.HandlePatientResolve, "/api/patient/resolve", map[string]any{
		"patientId": "12345",
		"office":    "Spring Hill",
	})
	var resolved struct {
		Appointments []struct {
			ID                int    `json:"id"`
			CancellationToken string `json:"cancellationToken"`
			RescheduleToken   string `json:"rescheduleToken"`
		} `json:"appointments"`
	}
	if err := json.NewDecoder(resolveRecorder.Body).Decode(&resolved); err != nil {
		t.Fatalf("decode patient resolution: %v", err)
	}
	if len(resolved.Appointments) != 2 ||
		resolved.Appointments[0].CancellationToken == "" ||
		resolved.Appointments[1].CancellationToken == "" ||
		resolved.Appointments[0].CancellationToken == resolved.Appointments[1].CancellationToken ||
		resolved.Appointments[0].RescheduleToken == "" ||
		resolved.Appointments[1].RescheduleToken == "" ||
		resolved.Appointments[0].RescheduleToken == resolved.Appointments[1].RescheduleToken ||
		resolved.Appointments[0].RescheduleToken == resolved.Appointments[0].CancellationToken {
		t.Fatalf("resolved appointments = %#v", resolved.Appointments)
	}

	cancelRecorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", map[string]any{
		"cancellationToken": resolved.Appointments[1].CancellationToken,
	})
	var cancelled CancelAppointmentResponse
	if err := json.NewDecoder(cancelRecorder.Body).Decode(&cancelled); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if cancelled.Status != "cancelled" || cancelled.AppointmentID != 44444 {
		t.Fatalf("cancellation response = %#v", cancelled)
	}
	if records.appointmentReads != 1 ||
		len(records.Cancellations) != 1 ||
		records.Cancellations[0].AppointmentID != 44444 {
		t.Fatalf("provider operations = reads %d, cancellations %#v", records.appointmentReads, records.Cancellations)
	}
}

func TestPatientResolutionKeepsCancellationTokenOptionalForOlderComposition(t *testing.T) {
	records := newCancellationTokenRecords()
	records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{{
				ID:       33333,
				Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
				OfficeID: "spring_hill",
				Office:   "Spring Hill",
			}},
			Complete: true,
		},
	}
	handlers := NewHandlers(nil, patient.New(records), nil)

	recorder := postJSON(t, handlers.HandlePatientResolve, "/api/patient/resolve", map[string]any{
		"patientId": "12345",
		"office":    "Spring Hill",
	})
	if strings.Contains(recorder.Body.String(), "cancellationToken") {
		t.Fatalf("legacy patient resolution exposed cancellationToken: %s", recorder.Body.String())
	}
	var response PatientResolveResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode patient resolution: %v", err)
	}
	if response.Status != "verified" || len(response.Appointments) != 1 {
		t.Fatalf("patient resolution = %#v", response)
	}
}

func TestPresentEmptyCancellationTokenDoesNotFallBackToRediscovery(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{{
				ID:       33333,
				Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
				OfficeID: "spring_hill",
				Office:   "Spring Hill",
			}},
			Complete: true,
		},
	}
	handlers := NewHandlers(
		nil,
		patient.New(records),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)

	recorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", map[string]any{
		"patientId":         "12345",
		"appointmentId":     33333,
		"office":            "Spring Hill",
		"cancellationToken": "",
	})
	var response CancelAppointmentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if response.Status != "error" || response.Outcome != string(scheduling.CategoryInvalidCancellationToken) {
		t.Fatalf("cancellation response = %#v", response)
	}
	if records.appointmentReads != 0 {
		t.Fatalf("appointment reads = %d, want no legacy fallback", records.appointmentReads)
	}
	if len(records.Cancellations) != 0 {
		t.Fatalf("provider cancellations = %#v, want none", records.Cancellations)
	}
}

func TestCancellationTokenRejectionsPerformNoProviderOperations(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	appointment := domain.PatientAppointment{
		ID:       33333,
		Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		OfficeID: "crystal_river",
		Office:   "Crystal River",
	}
	validToken := issueCancellationToken(t, "test-scheduling-secret", now, "12345", appointment)
	expiredToken := issueCancellationToken(
		t,
		"test-scheduling-secret",
		now.Add(-16*time.Minute),
		"12345",
		appointment,
	)
	bookingToken, err := scheduling.SignSlotToken("test-scheduling-secret", scheduling.SlotPolicy{
		OfficeID:      "crystal_river",
		Routing:       string(domain.RoutingBachOnly),
		ColumnID:      1513,
		ProfileID:     620,
		StartDatetime: "2026-06-03T09:00",
		Duration:      15,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("sign booking token: %v", err)
	}

	tests := []struct {
		name  string
		token string
		body  map[string]any
	}{
		{name: "malformed", token: "not-a-token"},
		{name: "tampered", token: validToken + "x"},
		{name: "expired", token: expiredToken},
		{name: "wrong purpose", token: bookingToken},
		{
			name:  "wrong patient",
			token: validToken,
			body:  map[string]any{"patientId": "99999", "appointmentId": 33333, "office": "Crystal River"},
		},
		{
			name:  "wrong appointment",
			token: validToken,
			body:  map[string]any{"patientId": "12345", "appointmentId": 44444, "office": "Crystal River"},
		},
		{
			name:  "wrong office",
			token: validToken,
			body:  map[string]any{"patientId": "12345", "appointmentId": 33333, "office": "Spring Hill"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			records := newCancellationTokenRecords()
			records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
				Read: advancedmd.AppointmentRead{
					Appointments: []domain.PatientAppointment{appointment},
					Complete:     true,
				},
			}
			handlers := NewHandlers(
				nil,
				patient.New(records),
				scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
			)
			body := test.body
			if body == nil {
				body = map[string]any{
					"patientId":     "12345",
					"appointmentId": 33333,
					"office":        "Crystal River",
				}
			}
			body["cancellationToken"] = test.token

			recorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", body)
			var response CancelAppointmentResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode cancellation: %v", err)
			}
			if response.Status != "error" ||
				response.Outcome != string(scheduling.CategoryInvalidCancellationToken) {
				t.Fatalf("cancellation response = %#v", response)
			}
			if records.appointmentReads != 0 {
				t.Fatalf("appointment reads = %d, want none", records.appointmentReads)
			}
			if len(records.Cancellations) != 0 {
				t.Fatalf("provider cancellations = %#v, want none", records.Cancellations)
			}
		})
	}
}

func TestCancellationAndBookingTokensAreNotInterchangeable(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	cancellationToken := issueCancellationToken(t, "test-scheduling-secret", now, "12345", domain.PatientAppointment{
		ID:       33333,
		Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		OfficeID: "spring_hill",
		Office:   "Spring Hill",
	})
	handlers := NewHandlers(
		nil,
		patient.New(records),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)

	recorder := postJSON(t, handlers.HandleBookAppointment, "/api/appointment/book", map[string]any{
		"patientId":         "12345",
		"bookingToken":      cancellationToken,
		"appointmentTypeId": 1007,
	})
	var response BookAppointmentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode booking: %v", err)
	}
	if response.Status != "error" || response.Outcome != string(scheduling.CategoryInvalidBookingToken) {
		t.Fatalf("booking response = %#v", response)
	}
	if records.DemographicCalls != 0 ||
		records.SchedulerSetupCalls != 0 ||
		len(records.ScheduleReadQueries) != 0 ||
		len(records.Bookings) != 0 {
		t.Fatalf("provider operations occurred: records = %#v", records.Adapter)
	}
}

func TestCancellationTelemetryReportsOperationBudgetWithoutSensitiveValues(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	token := issueCancellationToken(t, "test-scheduling-secret", now, "12345", domain.PatientAppointment{
		ID:       33333,
		Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		OfficeID: "spring_hill",
		Office:   "Spring Hill",
	})
	handlers := NewHandlers(
		nil,
		patient.New(records),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)
	router := NewRouter(handlers, "agent-secret", nil)
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousOutput)

	recorder := postAuthenticatedJSON(t, router, "/api/appointment/cancel", map[string]any{
		"patientId":         "12345",
		"appointmentId":     33333,
		"office":            "Spring Hill",
		"cancellationToken": token + "tampered",
	})
	var response CancelAppointmentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if response.Outcome != string(scheduling.CategoryInvalidCancellationToken) {
		t.Fatalf("cancellation response = %#v", response)
	}

	var entry struct {
		Cancellation struct {
			Path              string `json:"path"`
			Outcome           string `json:"outcome"`
			ScheduleReads     int    `json:"schedule_reads"`
			ProviderMutations int    `json:"provider_mutations"`
			DurationMS        int64  `json:"duration_ms"`
		} `json:"cancellation"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("decode cancellation telemetry %q: %v", logs.String(), err)
	}
	if entry.Cancellation.Path != "token" ||
		entry.Cancellation.Outcome != "invalid_cancellation_token" ||
		entry.Cancellation.ScheduleReads != 0 ||
		entry.Cancellation.ProviderMutations != 0 ||
		entry.Cancellation.DurationMS < 0 {
		t.Fatalf("cancellation telemetry = %#v", entry.Cancellation)
	}
	got := logs.String()
	for _, sensitive := range []string{token, "12345", "33333", "test-scheduling-secret"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("cancellation telemetry contains sensitive value %q: %s", sensitive, got)
		}
	}
}

func TestLegacyCancellationTelemetryReportsProviderReadCount(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
		Read: advancedmd.AppointmentRead{
			Appointments: []domain.PatientAppointment{{
				ID:       33333,
				Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
				OfficeID: "spring_hill",
				Office:   "Spring Hill",
			}},
			Complete:      true,
			ProviderReads: 12,
		},
	}
	handlers := NewHandlers(
		nil,
		patient.New(records),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)
	router := NewRouter(handlers, "agent-secret", nil)
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousOutput)

	recorder := postAuthenticatedJSON(t, router, "/api/appointment/cancel", map[string]any{
		"patientId":     "12345",
		"appointmentId": 33333,
		"office":        "Spring Hill",
	})
	var response CancelAppointmentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if response.Status != "cancelled" {
		t.Fatalf("cancellation response = %#v", response)
	}
	var entry struct {
		Cancellation struct {
			Path              string `json:"path"`
			ScheduleReads     int    `json:"schedule_reads"`
			ProviderMutations int    `json:"provider_mutations"`
		} `json:"cancellation"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("decode cancellation telemetry %q: %v", logs.String(), err)
	}
	if entry.Cancellation.Path != "legacy" ||
		entry.Cancellation.ScheduleReads != 12 ||
		entry.Cancellation.ProviderMutations != 1 {
		t.Fatalf("cancellation telemetry = %#v", entry.Cancellation)
	}
}

func TestCancellationHTTPContractSupportsTokenOnlyAndTokenlessLegacyRequests(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	appointment := domain.PatientAppointment{
		ID:       33333,
		Start:    time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC),
		OfficeID: "spring_hill",
		Office:   "Spring Hill",
	}

	t.Run("token only", func(t *testing.T) {
		records := newCancellationTokenRecords()
		token := issueCancellationToken(t, "test-scheduling-secret", now, "12345", appointment)
		handlers := NewHandlers(
			nil,
			patient.New(records),
			scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
		)
		recorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", map[string]any{
			"cancellationToken": token,
		})
		var response CancelAppointmentResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode cancellation: %v", err)
		}
		if response.Status != "cancelled" || response.AppointmentID != 33333 {
			t.Fatalf("cancellation response = %#v", response)
		}
		if records.appointmentReads != 0 || len(records.Cancellations) != 1 {
			t.Fatalf("provider operations = reads %d, cancellations %#v", records.appointmentReads, records.Cancellations)
		}
	})

	t.Run("tokenless legacy", func(t *testing.T) {
		records := newCancellationTokenRecords()
		records.AppointmentResults["12345"] = advancedmdtest.AppointmentResult{
			Read: advancedmd.AppointmentRead{
				Appointments: []domain.PatientAppointment{appointment},
				Complete:     true,
			},
		}
		handlers := NewHandlers(
			nil,
			patient.New(records),
			scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
		)
		recorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", map[string]any{
			"patientId":     "12345",
			"appointmentId": 33333,
			"office":        "Spring Hill",
		})
		var response CancelAppointmentResponse
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode cancellation: %v", err)
		}
		if response.Status != "cancelled" || response.AppointmentID != 33333 {
			t.Fatalf("cancellation response = %#v", response)
		}
		if records.appointmentReads != 1 || len(records.Cancellations) != 1 {
			t.Fatalf("provider operations = reads %d, cancellations %#v", records.appointmentReads, records.Cancellations)
		}
	})
}

func TestTokenCancellationPreservesAmbiguousWriteReconciliation(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	start := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	records := newCancellationTokenRecords()
	records.CancelAppointmentErr = advancedmd.NewAmbiguousWriteError(safeerrors.CategoryTimeout)
	records.AppointmentStateResults[33333] = advancedmdtest.AppointmentStateResult{
		State: advancedmd.AppointmentState{Complete: true},
	}
	token := issueCancellationToken(t, "test-scheduling-secret", now, "12345", domain.PatientAppointment{
		ID:       33333,
		Start:    start,
		OfficeID: "crystal_river",
		Office:   "Crystal River",
	})
	handlers := NewHandlers(
		nil,
		patient.New(records),
		scheduling.New(records, "test-scheduling-secret", func() time.Time { return now }),
	)

	recorder := postJSON(t, handlers.HandleCancelAppointment, "/api/appointment/cancel", map[string]any{
		"cancellationToken": token,
	})
	var response CancelAppointmentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if response.Status != "cancelled" || response.AppointmentID != 33333 {
		t.Fatalf("cancellation response = %#v", response)
	}
	if records.appointmentReads != 0 || len(records.Cancellations) != 1 {
		t.Fatalf("provider operations = reads %d, cancellations %#v", records.appointmentReads, records.Cancellations)
	}
	if len(records.AppointmentStateQueries) != 1 ||
		records.AppointmentStateQueries[0].OfficeID != "crystal_river" ||
		records.AppointmentStateQueries[0].Start != start {
		t.Fatalf("reconciliation query = %#v", records.AppointmentStateQueries)
	}
}

type cancellationTokenRecords struct {
	*advancedmdtest.Adapter
	appointmentReads int
}

func newCancellationTokenRecords() *cancellationTokenRecords {
	records := &cancellationTokenRecords{Adapter: advancedmdtest.NewAdapter()}
	records.Demographics["12345"] = domain.PatientDemographics{DOB: "01/15/1980"}
	return records
}

func issueCancellationToken(
	t *testing.T,
	secret string,
	now time.Time,
	patientID string,
	appointment domain.PatientAppointment,
) string {
	t.Helper()
	token, err := scheduling.NewAppointmentTokens(secret, func() time.Time { return now }).
		IssueCancellationToken(patientID, appointment)
	if err != nil {
		t.Fatalf("issue cancellation token: %v", err)
	}
	return token
}

func (r *cancellationTokenRecords) ReadPatientAppointments(
	ctx context.Context,
	query domain.PatientAppointmentsQuery,
) (advancedmd.AppointmentRead, error) {
	r.appointmentReads++
	return r.Adapter.ReadPatientAppointments(ctx, query)
}

func postJSON(
	t *testing.T,
	handler http.HandlerFunc,
	path string,
	body map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func postAuthenticatedJSON(
	t *testing.T,
	handler http.Handler,
	path string,
	body map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer agent-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
