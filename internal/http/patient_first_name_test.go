package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlePatientResolveFirstNameAndDOB(t *testing.T) {
	var searchedFirstName bool
	handlers := newPatientResolveTestHandlers(t, http.StatusOK, func(_ *http.Request, body []byte) {
		if strings.Contains(string(body), `"@action":"lookuppatient"`) {
			searchedFirstName = strings.Contains(string(body), `"@name":",Jane"`)
		}
	})
	req := httptest.NewRequest("POST", "/api/patient/resolve", strings.NewReader(`{"firstName":"Jane","dob":"01/15/1980","office":"Spring Hill"}`))
	w := httptest.NewRecorder()
	handlers.HandlePatientResolve(w, req)
	var body PatientResolveResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !searchedFirstName {
		t.Fatal("expected first-name-only provider lookup")
	}
	if body.Status != "verified" || body.PatientID != "123" {
		t.Fatalf("status=%s patientId=%s, want verified/123", body.Status, body.PatientID)
	}
}
