package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
)

type failingScheduling struct {
	schedulingmodule.Scheduling
	err error
}

func (s failingScheduling) Book(context.Context, schedulingmodule.BookCommand) (schedulingmodule.BookReceipt, error) {
	return schedulingmodule.BookReceipt{}, s.err
}

func (s failingScheduling) Cancel(context.Context, schedulingmodule.CancelCommand) (schedulingmodule.CancelReceipt, error) {
	return schedulingmodule.CancelReceipt{}, s.err
}

func (s failingScheduling) Reschedule(context.Context, schedulingmodule.BookCommand) (schedulingmodule.RescheduleReceipt, error) {
	return schedulingmodule.RescheduleReceipt{}, s.err
}

func (s failingScheduling) List(context.Context, schedulingmodule.ListCommand) (schedulingmodule.AvailabilityResponse, error) {
	return schedulingmodule.AvailabilityResponse{}, s.err
}

var writeRoutes = []string{"/api/appointment/book", "/api/appointment/cancel", "/api/appointment/reschedule"}

func replyTo(t *testing.T, router http.Handler, path, body string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+pinSecret)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d", path, response.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestWriteRoutesNameValidationFailures(t *testing.T) {
	router := newPinHarness(t).router
	request := func(path, body string) map[string]any {
		t.Helper()
		return replyTo(t, router, path, body)
	}
	for _, path := range writeRoutes {
		if got := request(path, `{"unknownField":true}`); got["outcome"] != "validation" {
			t.Errorf("%s with an unknown field: %v", path, got)
		}
	}
	if got := request("/api/appointment/cancel", `{}`); got["outcome"] != "validation" {
		t.Errorf("cancel with no token: %v", got)
	}
	slot := signedPinSlot(t, domain.DOB("01/15/1980"))
	if got := request("/api/appointment/book", `{"patientId":"abc","bookingToken":"`+slot+`","dob":"01/15/1980"}`); got["outcome"] != "validation" {
		t.Errorf("book with a bad patient: %v", got)
	}
}

func TestUntypedErrorsAreNeverNamedValidation(t *testing.T) {
	router := NewRouter(&Handlers{scheduling: failingScheduling{err: errors.New("untyped")}}, pinSecret, nil)
	for _, path := range writeRoutes {
		if got := replyTo(t, router, path, `{}`); got["outcome"] != "unclassified" {
			t.Errorf("%s: %v", path, got)
		}
	}
	if got := replyTo(t, router, "/api/appointment/reschedule", `{}`); got["status"] != "uncertain" {
		t.Errorf("an untyped reschedule error must not claim a failed move: %v", got)
	}
	got := replyTo(t, router, "/api/scheduler/slots", `{}`)
	if got["outcome"] != "availability_search_incomplete" || got["shouldRetrySameSearch"] != true {
		t.Errorf("slots: %v", got)
	}
}
