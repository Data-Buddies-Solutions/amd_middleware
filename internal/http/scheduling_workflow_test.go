package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/auth"
	"advancedmd-token-management/internal/clients"
	"advancedmd-token-management/internal/domain"
)

func TestSchedulingWorkflow_SignedAvailableSlotBooksWithSamePolicyAndReceipt(t *testing.T) {
	now := time.Now().UTC()
	searchDate := now.In(eastern).AddDate(0, 0, 2).Format("2006-01-02")
	workflow, bookingStatus, bookingPayload, _ := newSchedulingWorkflowTestHarness(t, now, searchDate, http.StatusOK)

	availability, workflowErr := workflow.Search(context.Background(), AvailabilityRequest{
		Date:    searchDate,
		Office:  "Spring Hill",
		Routing: string(domain.RoutingBachOnly),
		DOB:     "01/15/1980",
	}, now)
	if workflowErr != nil {
		t.Fatalf("Search error = %#v", workflowErr)
	}
	if availability.Outcome != domain.AvailabilityOutcomeFound || len(availability.Slots) != 1 {
		t.Fatalf("availability = %#v, want one returned slot", availability)
	}
	slot := availability.Slots[0]
	if slot.BookingToken == "" || !slot.RequiresForce || slot.SameStartBooked != 1 || slot.SameStartCapacity != 2 {
		t.Fatalf("signed slot = %#v, want same-start force metadata", slot)
	}
	payload, err := verifyBookingToken("test-booking-secret", slot.BookingToken, now.Add(time.Minute))
	if err != nil ||
		payload.DOB != "01/15/1980" ||
		payload.Routing != string(domain.RoutingBachOnly) ||
		!slices.Contains(payload.AppointmentTypeIDs, 1007) ||
		payload.SameStartBooked != 1 ||
		payload.SameStartCapacity != 2 {
		t.Fatalf("signed slot policy = %#v, err = %v", payload, err)
	}
	_, workflowErr = workflow.Book(context.Background(), BookAppointmentRequest{
		PatientID:         "123",
		DOB:               "not-a-date",
		BookingToken:      slot.BookingToken,
		AppointmentTypeID: 1007,
	}, now.Add(time.Minute))
	if workflowErr == nil || workflowErr.message != "dob must be a valid date" {
		t.Fatalf("invalid DOB error = %#v", workflowErr)
	}
	_, workflowErr = workflow.Book(context.Background(), BookAppointmentRequest{
		PatientID:         "123",
		DOB:               "01/15/2010",
		BookingToken:      slot.BookingToken,
		AppointmentTypeID: 1007,
	}, now.Add(time.Minute))
	if workflowErr == nil || workflowErr.outcome != "invalid_booking_token" {
		t.Fatalf("changed DOB error = %#v, want invalid_booking_token", workflowErr)
	}
	restrictedPolicy := payload
	restrictedPolicy.AppointmentTypeIDs = []int{1010}
	restrictedToken, err := signBookingToken("test-booking-secret", restrictedPolicy)
	if err != nil {
		t.Fatalf("sign restricted policy token: %v", err)
	}
	_, workflowErr = workflow.Book(context.Background(), BookAppointmentRequest{
		PatientID:         "123",
		BookingToken:      restrictedToken,
		AppointmentTypeID: 1007,
	}, now.Add(time.Minute))
	if workflowErr == nil || workflowErr.outcome != "invalid_booking_token" {
		t.Fatalf("changed appointment-type policy error = %#v", workflowErr)
	}

	receipt, workflowErr := workflow.Book(context.Background(), BookAppointmentRequest{
		PatientID:         "123",
		PatientName:       "SMITH,JANE",
		BookingToken:      slot.BookingToken,
		AppointmentTypeID: 1007,
	}, now.Add(time.Minute))
	if workflowErr != nil {
		t.Fatalf("Book error = %#v", workflowErr)
	}
	if receipt.Status != "booked" || receipt.AppointmentID != 98765 || receipt.PatientName != "Jane Smith" || receipt.ProviderName != "Dr. Austin Bach" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if (*bookingPayload)["force"] != float64(1) {
		t.Fatalf("booking force = %#v, payload = %#v", (*bookingPayload)["force"], *bookingPayload)
	}

	*bookingStatus = http.StatusConflict
	_, workflowErr = workflow.Book(context.Background(), BookAppointmentRequest{
		PatientID:         "123",
		BookingToken:      slot.BookingToken,
		AppointmentTypeID: 1007,
	}, now.Add(2*time.Minute))
	if workflowErr == nil || workflowErr.outcome != "slot_unavailable" {
		t.Fatalf("conflict error = %#v, want slot_unavailable", workflowErr)
	}
}

func TestSchedulingWorkflow_SearchReportsPartialAppointmentFailure(t *testing.T) {
	now := time.Now().UTC()
	searchDate := now.In(eastern).AddDate(0, 0, 2).Format("2006-01-02")
	workflow, _, _, partialFailure := newSchedulingWorkflowTestHarness(t, now, searchDate, http.StatusOK)
	*partialFailure = true
	workflow.schedulerSetup.Columns = append(workflow.schedulerSetup.Columns, domain.SchedulerColumn{
		ID:         "1598",
		Name:       "DR. BACH - SH 2",
		ProfileID:  "620",
		FacilityID: "1568",
		StartTime:  "09:00",
		EndTime:    "09:15",
		Interval:   15,
		Workweek:   127,
	})

	availability, workflowErr := workflow.Search(context.Background(), AvailabilityRequest{
		Date:    searchDate,
		Office:  "Spring Hill",
		Routing: string(domain.RoutingBachOnly),
		DOB:     "01/15/1980",
	}, now)
	if workflowErr != nil {
		t.Fatalf("Search error = %#v", workflowErr)
	}
	if availability.Status != domain.AvailabilityStatusError ||
		availability.Outcome != domain.AvailabilityOutcomeSearchIncomplete ||
		!availability.ShouldRetrySameSearch ||
		len(availability.Slots) != 0 {
		t.Fatalf("availability = %#v, want explicit incomplete-search outcome", availability)
	}
}

func TestSchedulingWorkflow_SearchRejectsIneligibleProvider(t *testing.T) {
	now := time.Now().UTC()
	searchDate := now.In(eastern).AddDate(0, 0, 2).Format("2006-01-02")
	workflow, _, _, _ := newSchedulingWorkflowTestHarness(t, now, searchDate, http.StatusOK)

	_, workflowErr := workflow.Search(context.Background(), AvailabilityRequest{
		Date:     searchDate,
		Office:   "Spring Hill",
		Routing:  string(domain.RoutingBachOnly),
		DOB:      "01/15/1980",
		Provider: "Dr. Licht",
	}, now)
	if workflowErr == nil || !strings.Contains(workflowErr.message, `No provider found matching "Dr. Licht"`) {
		t.Fatalf("provider error = %#v", workflowErr)
	}
}

func newSchedulingWorkflowTestHarness(t *testing.T, now time.Time, searchDate string, appointmentStatus int) (*schedulingWorkflow, *int, *map[string]any, *bool) {
	t.Helper()
	bookingStatus := http.StatusOK
	bookingPayload := map[string]any{}
	partialFailure := false

	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status := http.StatusOK
		response := `[]`
		contentType := request.Header.Get("Content-Type")

		switch {
		case strings.Contains(contentType, "application/xml") && strings.Contains(request.URL.Host, "partnerlogin"):
			response = `<PPMDResults><Results><usercontext webserver="https://mock.advancedmd.test/processrequest/api-801/APP"></usercontext></Results></PPMDResults>`
		case strings.Contains(contentType, "application/xml"):
			response = `<PPMDResults><Results success="1"><usercontext>test-token</usercontext></Results></PPMDResults>`
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/scheduler/appointments"):
			columnID := request.URL.Query().Get("columnId")
			requestedDate := request.URL.Query().Get("startDate")
			status = appointmentStatus
			if partialFailure && columnID == "1513" {
				status = http.StatusInternalServerError
				response = `{"error":"appointments unavailable"}`
			} else if partialFailure {
				response = fmt.Sprintf(`[
					{"id":1,"startdatetime":%q,"duration":15,"columnid":1598,"profileid":620,"patientid":998},
					{"id":2,"startdatetime":%q,"duration":15,"columnid":1598,"profileid":620,"patientid":999}
				]`, requestedDate+"T09:00", requestedDate+"T09:00")
			} else if status == http.StatusOK {
				response = fmt.Sprintf(`[{"id":1,"startdatetime":%q,"duration":15,"columnid":1513,"profileid":620,"patientid":999}]`, searchDate+"T09:00")
			} else {
				response = `{"error":"appointments unavailable"}`
			}
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/scheduler/blockholds"):
			response = `[]`
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "/scheduler/Appointments"):
			status = bookingStatus
			if status == http.StatusConflict {
				response = `{"error":"conflict"}`
				break
			}
			body, _ := io.ReadAll(request.Body)
			if err := json.Unmarshal(body, &bookingPayload); err != nil {
				t.Fatalf("decode booking payload: %v", err)
			}
			response = `{"id":98765}`
		default:
			status = http.StatusInternalServerError
			response = `{"error":"unexpected request"}`
		}

		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(response)),
			Request:    request,
		}, nil
	})}

	authenticator := auth.NewAuthenticator(auth.Credentials{
		Username:  "user",
		Password:  "pass",
		OfficeKey: "office",
		AppName:   "app",
	}, httpClient)
	workflow := newSchedulingWorkflow(
		auth.NewTokenManager(authenticator),
		nil,
		clients.NewAdvancedMDRestClient(httpClient),
		"test-booking-secret",
	)
	workflow.schedulerSetup = &domain.SchedulerSetup{
		Columns: []domain.SchedulerColumn{{
			ID:         "1513",
			Name:       "DR. BACH - SH",
			ProfileID:  "620",
			FacilityID: "1568",
			StartTime:  "09:00",
			EndTime:    "09:15",
			Interval:   15,
			Workweek:   127,
		}},
		Profiles:   []domain.SchedulerProfile{{ID: "620", Name: "BACH, AUSTIN"}},
		Facilities: []domain.SchedulerFacility{{ID: "1568", Name: "ABITA EYE GROUP SPRING HILL"}},
	}
	workflow.schedulerSetupExpiresAt = now.Add(time.Hour)
	return workflow, &bookingStatus, &bookingPayload, &partialFailure
}
