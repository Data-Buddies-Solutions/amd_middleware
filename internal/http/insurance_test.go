package http

import (
	"advancedmd-token-management/internal/domain"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInsuranceDecisionHTTPContract(t *testing.T) {
	handlers := &Handlers{}
	w := httptest.NewRecorder()
	handlers.HandleInsuranceDecision(w, httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(`{"plan":"United Individual Exchange","coverageType":"medical","office":"Hollywood"}`)))
	var d domain.InsuranceDecision
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.CarrierCode != "UNI20" || d.Participation != "accepted" || d.CanSchedule || len(d.Requirements) != 1 || d.Requirements[0].Verification != "unverified" {
		t.Fatalf("response=%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "carrierId") {
		t.Fatal("provider transport ID exposed")
	}
	// A caller assertion is not a staff or payer verification record.
	w = httptest.NewRecorder()
	handlers.HandleInsuranceDecision(w, httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(`{"plan":"United Individual Exchange","coverageType":"medical","office":"Hollywood","referralVerified":true}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatal("caller verification flag accepted")
	}
}
