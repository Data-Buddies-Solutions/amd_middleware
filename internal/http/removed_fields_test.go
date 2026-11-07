package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemovedWireFieldsAreRefusedByTheStrictDecoders(t *testing.T) {
	router := newPinHarness(t).router
	for path, fields := range map[string][]string{
		"/api/scheduler/slots":          {`"routing":"all_three"`, `"preauthRequired":true`},
		"/api/appointment/book":         {`"appointmentTypeId":1007`, `"office":"Spring Hill"`},
		"/api/appointment/reschedule":   {`"appointmentTypeId":1007`, `"office":"Spring Hill"`},
		"/api/appointment/cancel":       {`"appointmentId":33333`, `"office":"Spring Hill"`},
		"/api/patient/resolve":          {`"lastName":"Doe"`},
		"/api/add-patient":              {`"insurance":"Aetna"`},
		"/api/patient/update-insurance": {`"insurance":"Aetna"`},
	} {
		for _, field := range fields {
			got := replyTo(t, router, path, "{"+field+"}")
			if got["message"] != "Invalid JSON body" {
				t.Errorf("%s accepted %s: %v", path, field, got)
			}
		}
	}
}

func TestInsuranceDecisionRouteIsGone(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+pinSecret)
	response := httptest.NewRecorder()
	newPinHarness(t).router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}
