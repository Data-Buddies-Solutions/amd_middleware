package http

import (
	"encoding/json"
	"net/http"

	"advancedmd-token-management/internal/safeerrors"
)

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func rejectInvalidRequest(w http.ResponseWriter, r *http.Request, response any) {
	recordRequestOutcome(r.Context(), outcomeInvalidRequest, safeerrors.CategoryNone)
	respond(w, response)
}

func decodeStrict(r *http.Request, v any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}
