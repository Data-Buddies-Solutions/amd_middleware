package eligibility

import (
	"encoding/json"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func planResult(plans ...string) Result {
	benefits := []map[string]any{}
	for _, plan := range plans {
		benefits = append(benefits, map[string]any{"code": "1", "serviceTypeCodes": []string{"30"}, "planCoverage": plan})
	}
	raw, _ := json.Marshal(map[string]any{"meta": map[string]string{"applicationMode": "production"}, "benefitsInformation": benefits})
	return Result{Match: &MatchResult{Status: "exact_name_dob"}, MatchedPatient: &Person{FirstName: "Jane", LastName: "Doe"}, ProviderResponse: raw}
}

func TestResolveInsurance(t *testing.T) {
	office, _ := domain.LookupOfficeByID("spring_hill")
	for _, tc := range []struct {
		name         string
		result       Result
		status, plan string
	}{
		{"specific", planResult("Aetna Better Health"), "resolved", "Aetna Better Health"},
		{"alias", planResult("Aetna Employer Plan"), "resolved", "Aetna Commercial"},
		{"unknown carrier product", planResult("Aetna Unknown Product"), "unmapped", ""},
		{"conflict", planResult("Aetna Better Health", "Aetna Commercial"), "conflicting", ""},
		{"missing", planResult(), "unavailable", ""},
		{"provider conflict", Result{ProviderResults: []Result{planResult("Aetna Better Health"), planResult("Aetna Commercial")}}, "conflicting", ""},
		{"partial provider failure retains specific evidence", Result{ProviderResults: []Result{planResult("Aetna Better Health"), {Status: "unknown"}}}, "resolved", "Aetna Better Health"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveInsurance(tc.result, office, CheckInput{CoverageType: "medical", DOB: "01/02/1980"})
			if got.Status != tc.status {
				t.Fatalf("got %+v", got)
			}
			if tc.plan != "" && (got.Decision == nil || got.Decision.CanonicalPlan != tc.plan) {
				t.Fatalf("decision %+v", got.Decision)
			}
		})
	}
	crystal, _ := domain.LookupOfficeByID("crystal_river")
	got := ResolveInsurance(planResult("Aetna Better Health"), crystal, CheckInput{CoverageType: "medical"})
	if got.Status != "resolved" || got.Decision.Participation != "not_accepted" {
		t.Fatalf("rejected plan: %+v", got)
	}
}

func TestUntrustedAndServiceSpecificPlanCannotReplaceInsurance(t *testing.T) {
	office, _ := domain.LookupOfficeByID("spring_hill")
	for _, kind := range []string{"identity", "test mode", "service", "copay", "payer error"} {
		t.Run(kind, func(t *testing.T) {
			result := planResult("Aetna Better Health")
			var response map[string]any
			_ = json.Unmarshal(result.ProviderResponse, &response)
			benefit := response["benefitsInformation"].([]any)[0].(map[string]any)
			switch kind {
			case "identity":
				result.Match.ReviewRequired = true
			case "test mode":
				response["meta"] = map[string]string{"applicationMode": "test"}
			case "service":
				benefit["serviceTypeCodes"] = []string{"98"}
			case "copay":
				benefit["code"] = "B"
			case "payer error":
				response["errors"] = []map[string]string{{"code": "72"}}
			}
			result.ProviderResponse, _ = json.Marshal(response)
			if got := ResolveInsurance(result, office, CheckInput{CoverageType: "medical"}); got.Status != "unavailable" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestDocumentedPlanDescriptions(t *testing.T) {
	office, _ := domain.LookupOfficeByID("spring_hill")
	for _, field := range []string{"planInformation", "benefitsAdditionalInformation"} {
		t.Run(field, func(t *testing.T) {
			result := planResult()
			var response map[string]any
			_ = json.Unmarshal(result.ProviderResponse, &response)
			description := map[string]string{"planDescription": "Aetna Better Health"}
			if field == "planInformation" {
				response[field] = description
			} else {
				response["benefitsInformation"] = []map[string]any{{"code": "1", "serviceTypeCodes": []string{"30"}, field: description}}
			}
			result.ProviderResponse, _ = json.Marshal(response)
			got := ResolveInsurance(result, office, CheckInput{CoverageType: "medical"})
			if got.Status != "resolved" || got.Decision.CanonicalPlan != "Aetna Better Health" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
