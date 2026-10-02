package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
)

func TestInsuranceDecisionHTTPContract(t *testing.T) {
	handlers := &Handlers{}
	office, _ := domain.ResolveOffice("Hollywood")
	for _, plan := range []string{"Self Pay", "Aetna", "unknown words"} {
		w := httptest.NewRecorder()
		body := `{"plan":"` + plan + `","coverageType":"medical","office":"Hollywood","dob":"01/15/1980"}`
		handlers.HandleInsuranceDecision(w, httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(body)))
		var got insurance.InsuranceDecision
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		want := insurance.DecideInsurance(plan, "medical", office, "01/15/1980")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: response=%s want=%+v", plan, w.Body.String(), want)
		}
		if strings.Contains(w.Body.String(), "routing") || strings.Contains(w.Body.String(), "credentialedProviders") {
			t.Fatalf("dropped field returned: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	handlers.HandleInsuranceDecision(w, httptest.NewRequest(http.MethodPost, "/api/insurance/decision", strings.NewReader(`{"plan":"Self Pay","coverageType":"medical","office":"Hollywood","referralVerified":true}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatal("caller verification flag accepted")
	}
}
