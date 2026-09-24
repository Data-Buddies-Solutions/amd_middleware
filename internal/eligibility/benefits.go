package eligibility

import (
	"encoding/json"
	"slices"
)

// Filter benefit rows once at the middleware boundary. Keep complete matching
// rows, including network, tier, plan descriptions and unknown payer qualifiers.
// Assess the original response separately so filtering cannot hide failures.
func retainVisitBenefits(raw json.RawMessage, coverage string) json.RawMessage {
	var response map[string]json.RawMessage
	if json.Unmarshal(raw, &response) != nil || response == nil {
		return raw
	}
	// Raw X12 duplicates unfiltered benefits outside the selected JSON rows.
	delete(response, "x12")
	visit := "98"
	if coverage == "routine_vision" {
		visit = "AL"
	}
	for _, field := range []string{"benefitsInformation", "planStatus"} {
		var rows []json.RawMessage
		if value, ok := response[field]; !ok || json.Unmarshal(value, &rows) != nil {
			continue
		}
		retained := make([]json.RawMessage, 0, len(rows))
		for _, row := range rows {
			var benefit struct {
				Codes []string `json:"serviceTypeCodes"`
			}
			if json.Unmarshal(row, &benefit) != nil {
				continue
			}
			if slices.Contains(benefit.Codes, "30") || slices.Contains(benefit.Codes, visit) {
				retained = append(retained, row)
			}
		}
		response[field], _ = json.Marshal(retained)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return raw
	}
	return encoded
}
