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
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Invalid insurance request"})
		return
	}
	office, err := domain.ResolveOffice(req.Office)
	if err != nil {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Unknown office"})
		return
	}
	respond(w, insurance.DecideOfferedAnswer(req.Plan, req.Offered, req.CoverageType, office, req.DOB, h.now()))
}

func (h *Handlers) HandleInsurancePlans(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	officeKey := query.Get("office")
	office, err := domain.ResolveOffice(officeKey)
	if officeKey == "" || err != nil {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Unknown office"})
		return
	}
	coverage := query.Get("coverage")
	plans, ok := insurance.ListPlans(coverage, office)
	if !ok {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Unknown coverage"})
		return
	}
	respond(w, struct {
		OfficeID string                  `json:"officeId"`
		Coverage string                  `json:"coverage"`
		Plans    []insurance.PlanSummary `json:"plans"`
	}{office.ID, coverage, plans})
}
