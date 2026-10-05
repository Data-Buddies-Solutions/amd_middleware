package http

import (
	"net/http"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
)

func (h *Handlers) HandleInsuranceDecision(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plan         string   `json:"plan"`
		CoverageType string   `json:"coverageType"`
		Office       string   `json:"office"`
		DOB          string   `json:"dob"`
		Offered      []string `json:"offeredPlanIds"`
	}
	if decodeStrict(r, &req) != nil {
		http.Error(w, "Invalid insurance request", http.StatusBadRequest)
		return
	}
	office, err := domain.ResolveOffice(req.Office)
	if err != nil {
		http.Error(w, "Unknown office", http.StatusBadRequest)
		return
	}
	respond(w, insurance.DecideOfferedAnswer(req.Plan, req.Offered, req.CoverageType, office, req.DOB))
}

func (h *Handlers) HandleInsurancePlans(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	officeKey := query.Get("office")
	office, err := domain.ResolveOffice(officeKey)
	if officeKey == "" || err != nil {
		http.Error(w, "Unknown office", http.StatusBadRequest)
		return
	}
	coverage := query.Get("coverage")
	plans, ok := insurance.ListPlans(coverage, office)
	if !ok {
		http.Error(w, "Unknown coverage", http.StatusBadRequest)
		return
	}
	respond(w, struct {
		OfficeID string                  `json:"officeId"`
		Coverage string                  `json:"coverage"`
		Plans    []insurance.PlanSummary `json:"plans"`
	}{office.ID, coverage, plans})
}
