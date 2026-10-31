package http

import (
	"encoding/json"
	"io"
	"net/http"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/safeerrors"
)

func (h *Handlers) HandleInsuranceDecision(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plan         string   `json:"plan"`
		CoverageType string   `json:"coverageType"`
		Office       string   `json:"office"`
		DOB          string   `json:"dob"`
		Offered      []string `json:"offeredPlanIds"`
		SelfPay      bool     `json:"selfPay"`
	}
	if decodeStrict(r, &req) != nil || (req.SelfPay && (req.Plan != "" || len(req.Offered) > 0)) {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Invalid insurance request"})
		return
	}
	office, err := domain.ResolveOffice(req.Office)
	if err != nil {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: "Unknown office"})
		return
	}
	now := h.now()
	dob, err := domain.ParseDOB(req.DOB, now)
	if err != nil {
		rejectInvalidRequest(w, r, errorResponse{Status: "error", Message: err.Error()})
		return
	}
	if req.SelfPay {
		respond(w, insurance.DecideSelfPay(req.CoverageType, office, string(dob), now))
		return
	}
	respond(w, insurance.DecideOfferedAnswer(req.Plan, req.Offered, req.CoverageType, office, string(dob), now))
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
	respond(w, plans)
}

type catalogVerdict struct {
	Valid  bool                     `json:"valid"`
	Errors []insurance.CatalogError `json:"errors,omitempty"`
}

func (h *Handlers) HandleValidateInsuranceCatalog(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rejectCatalog(w, r, []insurance.CatalogError{{Rule: "invalid_json", Message: err.Error()}})
		return
	}
	candidate, err := insurance.DecodeCatalog(body)
	if err != nil {
		rejectCatalog(w, r, []insurance.CatalogError{{Rule: "invalid_json", Message: err.Error()}})
		return
	}
	if errs := insurance.ValidateCatalog(candidate); len(errs) > 0 {
		rejectCatalog(w, r, errs)
		return
	}
	respond(w, catalogVerdict{Valid: true})
}

func rejectCatalog(w http.ResponseWriter, r *http.Request, errs []insurance.CatalogError) {
	recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(catalogVerdict{Errors: errs})
}
