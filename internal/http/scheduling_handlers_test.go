package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
)

func TestBookingAndCancellationHandlersDelegateToScheduling(t *testing.T) {
	scheduler := &recordingScheduling{
		bookReceipt: schedulingmodule.BookReceipt{
			Status:              "booked",
			AppointmentID:       98765,
			PatientID:           "12345",
			ProviderName:        "Dr. Austin Bach",
			LocationName:        "Spring Hill",
			StartDatetime:       "2026-06-03T09:00",
			Duration:            15,
			AppointmentTypeID:   1007,
			AppointmentTypeName: "Established Adult Medical (Follow Up)",
			Message:             "Appointment booked successfully",
		},
		cancelReceipt: schedulingmodule.CancelReceipt{
			Status:        "cancelled",
			AppointmentID: 98765,
			Message:       "Appointment cancelled successfully",
		},
	}
	handlers := &Handlers{scheduling: scheduler}

	bookRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/appointment/book",
		strings.NewReader(`{"patientId":"12345","bookingToken":"signed-slot","appointmentTypeId":1007}`),
	)
	bookResponse := httptest.NewRecorder()
	handlers.HandleBookAppointment(bookResponse, bookRequest)

	var book schedulingmodule.BookReceipt
	if err := json.NewDecoder(bookResponse.Body).Decode(&book); err != nil {
		t.Fatalf("decode booking response: %v", err)
	}
	if scheduler.bookCommand.PatientID != "12345" ||
		scheduler.bookCommand.BookingToken != "signed-slot" ||
		book.Status != scheduler.bookReceipt.Status ||
		book.AppointmentID != scheduler.bookReceipt.AppointmentID ||
		book.Message != scheduler.bookReceipt.Message {
		t.Fatalf("booking command = %#v, response = %#v", scheduler.bookCommand, book)
	}

	cancelRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/appointment/cancel",
		strings.NewReader(`{"patientId":"pat12345","appointmentId":98765,"office":"Spring Hill"}`),
	)
	cancelResponse := httptest.NewRecorder()
	handlers.HandleCancelAppointment(cancelResponse, cancelRequest)

	var cancel schedulingmodule.CancelReceipt
	if err := json.NewDecoder(cancelResponse.Body).Decode(&cancel); err != nil {
		t.Fatalf("decode cancellation response: %v", err)
	}
	if scheduler.cancelCommand.PatientID != "pat12345" ||
		scheduler.cancelCommand.AppointmentID != 98765 ||
		cancel != scheduler.cancelReceipt {
		t.Fatalf("cancellation command = %#v, response = %#v", scheduler.cancelCommand, cancel)
	}
}

func TestAvailabilityHandlerPreservesRequestedDateAndPreferredTime(t *testing.T) {
	scheduler := &recordingScheduling{
		searchResponse: domain.AvailabilityResponse{
			Status:  domain.AvailabilityStatusSuccess,
			Outcome: domain.AvailabilityOutcomeNoAvailability,
			Slots:   []domain.AvailabilitySlotOption{},
		},
	}
	handlers := &Handlers{scheduling: scheduler}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/scheduler/availability",
		strings.NewReader(`{"requestedDate":"2026-06-03","office":"Spring Hill","preferredTime":{"minuteOfDay":900}}`),
	)
	response := httptest.NewRecorder()

	handlers.HandleGetAvailability(response, request)

	var body domain.AvailabilityResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode availability response: %v", err)
	}
	if scheduler.searchCommand.RequestedDate != "2026-06-03" ||
		scheduler.searchCommand.PreferredTime == nil ||
		scheduler.searchCommand.PreferredTime.Kind != "" ||
		scheduler.searchCommand.PreferredTime.MinuteOfDay == nil ||
		*scheduler.searchCommand.PreferredTime.MinuteOfDay != 900 {
		t.Fatalf("search command = %#v, response = %#v", scheduler.searchCommand, body)
	}
}

func TestAvailabilityHandlerRejectsLegacyDate(t *testing.T) {
	scheduler := &recordingScheduling{}
	handlers := &Handlers{scheduling: scheduler}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/scheduler/availability",
		strings.NewReader(`{"date":"2026-06-03","office":"Spring Hill"}`),
	)
	response := httptest.NewRecorder()

	handlers.HandleGetAvailability(response, request)

	var body ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode availability response: %v", err)
	}
	if body.Status != "error" || scheduler.searchCalls != 0 {
		t.Fatalf("response = %#v, search calls = %d", body, scheduler.searchCalls)
	}
}

func TestBookingAndCancellationRoutesRetainAuthenticationAndSuccessContracts(t *testing.T) {
	scheduler := &recordingScheduling{
		bookReceipt: schedulingmodule.BookReceipt{
			Status:        "booked",
			AppointmentID: 98765,
			Message:       "Appointment booked successfully",
		},
		cancelReceipt: schedulingmodule.CancelReceipt{
			Status:        "cancelled",
			AppointmentID: 98765,
			Message:       "Appointment cancelled successfully",
		},
	}
	router := NewRouter(&Handlers{scheduling: scheduler}, "agent-secret", nil)
	tests := []struct {
		path   string
		body   string
		status string
	}{
		{
			path:   "/api/appointment/book",
			body:   `{"patientId":"12345","bookingToken":"signed-slot","appointmentTypeId":1007}`,
			status: "booked",
		},
		{
			path:   "/api/appointment/cancel",
			body:   `{"patientId":"12345","appointmentId":98765,"office":"Spring Hill"}`,
			status: "cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			unauthenticated := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			unauthenticatedResponse := httptest.NewRecorder()
			router.ServeHTTP(unauthenticatedResponse, unauthenticated)
			if unauthenticatedResponse.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status = %d", unauthenticatedResponse.Code)
			}

			authenticated := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			authenticated.Header.Set("Authorization", "Bearer agent-secret")
			authenticatedResponse := httptest.NewRecorder()
			router.ServeHTTP(authenticatedResponse, authenticated)
			var response struct {
				Status        string `json:"status"`
				AppointmentID int    `json:"appointmentId"`
			}
			if err := json.NewDecoder(authenticatedResponse.Body).Decode(&response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if authenticatedResponse.Code != http.StatusOK ||
				response.Status != tt.status ||
				response.AppointmentID != 98765 {
				t.Fatalf("authenticated response = %d %#v", authenticatedResponse.Code, response)
			}
		})
	}
}

func TestSchedulingHandlersMapStableErrorCategories(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	_, bookingErr := schedulingmodule.New(nil, "test-secret", func() time.Time { return now }).
		Book(context.Background(), schedulingmodule.BookCommand{})
	_, cancellationErr := schedulingmodule.New(nil, "test-secret", func() time.Time { return now }).
		Cancel(context.Background(), schedulingmodule.CancelCommand{})
	scheduler := &recordingScheduling{bookErr: bookingErr, cancelErr: cancellationErr}
	handlers := &Handlers{scheduling: scheduler}

	bookResponse := httptest.NewRecorder()
	handlers.HandleBookAppointment(
		bookResponse,
		httptest.NewRequest(http.MethodPost, "/api/appointment/book", strings.NewReader(`{}`)),
	)
	var book schedulingmodule.BookReceipt
	json.NewDecoder(bookResponse.Body).Decode(&book)
	if book.Status != "error" || book.Outcome != "booking_token_required" {
		t.Fatalf("booking response = %#v", book)
	}

	cancelResponse := httptest.NewRecorder()
	handlers.HandleCancelAppointment(
		cancelResponse,
		httptest.NewRequest(http.MethodPost, "/api/appointment/cancel", strings.NewReader(`{}`)),
	)
	var cancel schedulingmodule.CancelReceipt
	json.NewDecoder(cancelResponse.Body).Decode(&cancel)
	if cancel.Status != "error" || cancel.Outcome != "" || cancel.Message != "appointmentId is required" {
		t.Fatalf("cancellation response = %#v", cancel)
	}
}

type recordingScheduling struct {
	listCommand    schedulingmodule.ListCommand
	listCalls      int
	searchCommand  schedulingmodule.SearchCommand
	searchResponse domain.AvailabilityResponse
	searchCalls    int
	bookCommand    schedulingmodule.BookCommand
	bookReceipt    schedulingmodule.BookReceipt
	bookErr        error
	cancelCommand  schedulingmodule.CancelCommand
	cancelReceipt  schedulingmodule.CancelReceipt
	cancelErr      error
}

func (s *recordingScheduling) Search(_ context.Context, command schedulingmodule.SearchCommand) (domain.AvailabilityResponse, error) {
	s.searchCalls++
	s.searchCommand = command
	return s.searchResponse, nil
}

func (s *recordingScheduling) Book(_ context.Context, command schedulingmodule.BookCommand) (schedulingmodule.BookReceipt, error) {
	s.bookCommand = command
	return s.bookReceipt, s.bookErr
}

func (s *recordingScheduling) Cancel(_ context.Context, command schedulingmodule.CancelCommand) (schedulingmodule.CancelReceipt, error) {
	s.cancelCommand = command
	return s.cancelReceipt, s.cancelErr
}

func (s *recordingScheduling) List(ctx context.Context, command schedulingmodule.ListCommand) (domain.AvailabilityResponse, error) {
	s.listCommand = command
	s.listCalls++
	return s.searchResponse, nil
}

func TestListSlotsRouteRequiresAuthenticationAndPreservesRange(t *testing.T) {
	scheduler := &recordingScheduling{searchResponse: domain.AvailabilityResponse{Status: "success", Outcome: "no_availability", Slots: []domain.AvailabilitySlotOption{}}}
	router := NewRouter(&Handlers{scheduling: scheduler}, "test-api-secret", nil)
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/api/scheduler/slots", strings.NewReader(`{"rangeDays":30,"office":"Spring Hill","routing":"bach_only","dob":"01/15/1980","preauthRequired":true}`))
		if authenticated {
			request.Header.Set("Authorization", "test-api-secret")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if !authenticated {
			if response.Code != http.StatusUnauthorized || scheduler.listCalls != 0 {
				t.Fatalf("unauthenticated status=%d calls=%d", response.Code, scheduler.listCalls)
			}
			continue
		}
		if response.Code != http.StatusOK || scheduler.listCalls != 1 || scheduler.listCommand.RangeDays != 30 || scheduler.listCommand.DOB != "01/15/1980" || !scheduler.listCommand.PreauthRequired {
			t.Fatalf("status=%d command=%#v", response.Code, scheduler.listCommand)
		}
	}
}

func TestListSlotsRejectsOldPreferenceContract(t *testing.T) {
	scheduler := &recordingScheduling{}
	handlers := &Handlers{scheduling: scheduler}
	request := httptest.NewRequest(http.MethodPost, "/api/scheduler/slots", strings.NewReader(`{"requestedDate":"2026-06-03"}`))
	response := httptest.NewRecorder()
	handlers.HandleListAppointmentSlots(response, request)
	if scheduler.listCalls != 0 || !strings.Contains(response.Body.String(), "Invalid JSON body") {
		t.Fatalf("old contract accepted: %s", response.Body.String())
	}
}
