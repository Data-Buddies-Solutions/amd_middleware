package http

import (
	"encoding/json"
	"io"
	"net/http"

	"advancedmd-token-management/internal/eligibility"
	"advancedmd-token-management/internal/safeerrors"
)

func (h *Handlers) SetEligibility(service *eligibility.Service) { h.eligibility = service }

func (h *Handlers) HandleEligibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if h.eligibility == nil {
		http.Error(w, `{"error":"eligibility_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var input eligibility.CheckInput
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
	result, err := h.eligibility.Check(r.Context(), input)
	if err != nil {
		http.Error(w, `{"error":"invalid_eligibility_input_or_history"}`, http.StatusBadRequest)
		return
	}
	// A receipt is still HTTP 200 when the payer outcome is unknown. Preserve
	// that contract while exposing failures through the existing safe logs.
	if result.Status == "unknown" {
		category := safeerrors.CategoryUpstreamError
		switch result.ReviewReason {
		case "stedi_http_failure":
			category = safeerrors.CategoryUpstreamStatus
		case "unrecognized_response", "search_chain_mismatch", "nonproduction_or_unknown_mode":
			category = safeerrors.CategoryInvalidResponse
		}
		recordRequestOutcome(r.Context(), outcomeProviderFailure, category)
	} else if result.Status == "payer_rejected" {
		recordRequestOutcome(r.Context(), outcomeProviderFailure, safeerrors.CategoryRejected)
	}
	_ = json.NewEncoder(w).Encode(result)
}
