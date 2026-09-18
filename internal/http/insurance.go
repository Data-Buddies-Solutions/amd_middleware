package http

import (
	"advancedmd-token-management/internal/domain"
	"encoding/json"
	"net/http"
)

func (h *Handlers) HandleInsuranceDecision(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Plan         string `json:"plan"`
		CoverageType string `json:"coverageType"`
		Office       string `json:"office"`
		DOB          string `json:"dob"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&req) != nil {
		http.Error(w, "Invalid insurance request", http.StatusBadRequest)
		return
	}
	office, err := domain.ResolveOffice(req.Office)
	if err != nil {
		http.Error(w, "Unknown office", http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(domain.DecideInsurance(req.Plan, req.CoverageType, office, req.DOB))
}
