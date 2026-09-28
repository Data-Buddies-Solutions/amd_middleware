package http

import (
	"encoding/json"
	"io"
	"net/http"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/eligibility"
	"advancedmd-token-management/internal/safeerrors"
)

func (h *Handlers) HandleEligibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.eligibility == nil {
		http.Error(w, `{"error":"eligibility_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var input struct {
		eligibility.CheckInput
		Office string `json:"office,omitempty"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid_eligibility_input"}`, http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		http.Error(w, `{"error":"invalid_eligibility_input"}`, http.StatusBadRequest)
		return
	}
	office, err := domain.ResolveOffice(input.Office)
	if err != nil {
		http.Error(w, `{"error":"invalid_office"}`, http.StatusBadRequest)
		return
	}
	result, err := h.eligibility.Check(r.Context(), office.ID, input.CheckInput)
	if err != nil {
		http.Error(w, `{"error":"invalid_eligibility_input"}`, http.StatusBadRequest)
		return
	}
	result.InsuranceResolution = eligibility.ResolveInsurance(result, office, input.CheckInput)
	if category := result.ProviderFailure(); category != safeerrors.CategoryNone {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, category)
	}
	respond(w, result)
}
