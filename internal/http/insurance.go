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
