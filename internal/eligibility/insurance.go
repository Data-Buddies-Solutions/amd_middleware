package eligibility

import (
	"encoding/json"
	"slices"
	"strings"

	"advancedmd-token-management/internal/domain"
)

// InsuranceResolution maps trusted general-plan descriptions, not payer IDs or
// service-specific benefit labels. Missing evidence leaves intake policy intact.
type InsuranceResolution struct {
	Status   string                    `json:"status"`
	Plans    []string                  `json:"plans"`
	Decision *domain.InsuranceDecision `json:"decision,omitempty"`
}

func ResolveInsurance(result Result, office *domain.OfficeConfig, input CheckInput) *InsuranceResolution {
	out := &InsuranceResolution{Status: "unavailable", Plans: []string{}}
	results := result.ProviderResults
	if len(results) == 0 {
		results = []Result{result}
	}
	coverage := input.CoverageType
	if coverage == "" {
		coverage = "medical"
	}
	seen := map[string]bool{}
	add := func(plan string) {
		plan = strings.TrimSpace(plan)
		key := strings.ToLower(strings.Join(strings.Fields(plan), " "))
		if key != "" && !seen[key] {
			seen[key] = true
			out.Plans = append(out.Plans, plan)
		}
	}
	for _, child := range results {
		if child.Match == nil || child.Match.ReviewRequired || child.MatchedPatient == nil {
			continue
		}
		var response Response
		if json.Unmarshal(child.ProviderResponse, &response) != nil || response.Meta.Mode != "production" || len(response.Errors) != 0 {
			continue
		}
		add(response.PlanInformation.Description)
		for _, benefit := range response.Benefits {
			if !slices.Contains(benefit.Services, "30") || len(benefit.Code) != 1 || !strings.Contains("12345678", benefit.Code) {
				continue
			}
			add(benefit.Plan)
			add(benefit.Additional.Description)
		}
	}
	unmapped := false
	for _, plan := range out.Plans {
		decision := domain.DecideEligibilityInsurance(plan, coverage, office, input.DOB)
		if decision.Outcome == "needs_staff_task" && decision.Participation == "unknown" {
			out.Status, out.Decision = "resolved", &decision
			return out
		}
		if decision.Participation == "unknown" || decision.SelfPay {
			unmapped = true
			continue
		}
		if decision.Participation == "not_accepted" {
			out.Status, out.Decision = "resolved", &decision
			return out
		}
		if out.Decision != nil && (out.Decision.CanonicalPlan != decision.CanonicalPlan || out.Decision.Participation != decision.Participation || out.Decision.CarrierCode != decision.CarrierCode) {
			out.Status, out.Decision = "conflicting", nil
			return out
		}
		out.Status, out.Decision = "resolved", &decision
	}
	if unmapped {
		out.Status, out.Decision = "unmapped", nil
	}
	return out
}
