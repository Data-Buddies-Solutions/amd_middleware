package http

import (
	"encoding/json"
	"io"
	"net/http"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/insurance"
	"advancedmd-token-management/internal/safeerrors"
)

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
