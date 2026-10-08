package insurance

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"advancedmd-token-management/internal/domain"
)

func decideCandidates(d InsuranceDecision, plans []plan, office *domain.OfficeConfig, dob string) InsuranceDecision {
	groups := distinctOutcomes(plans, office, dob)
	if len(groups) == 1 {
		return groups[0]
	}
	if len(groups) > maxOptions {
		d.Reason, d.Answer = "ask_full_name", answerAskFullName
		return d
	}
	labels := []string{}
	for _, group := range groups {
		d.Options = append(d.Options, InsuranceOption{PlanID: group.PlanID, Label: group.CanonicalPlan})
		labels = append(labels, group.CanonicalPlan)
	}
	d.Reason = "choose_plan"
	d.Answer = "needs_input: Which of these is on your card: " + joinWithOr(labels) + "?"
	return d
}

func distinctOutcomes(plans []plan, office *domain.OfficeConfig, dob string) []InsuranceDecision {
	plans = slices.Clone(plans)
	slices.SortStableFunc(plans, func(a, b plan) int {
		return cmp.Or(cmp.Compare(len(a.Label), len(b.Label)), cmp.Compare(a.Label, b.Label))
	})
	var groups []InsuranceDecision
	seen := map[string]bool{}
	for _, p := range plans {
		decision := decidePlanAtOffice(p, office, dob)
		key := outcomeKey(decision)
		if !seen[key] {
			seen[key] = true
			groups = append(groups, decision)
		}
	}
	return groups
}

func outcomeKey(d InsuranceDecision) string {
	doctors := slices.Clone(d.AllowedProviders)
	slices.Sort(doctors)
	kinds := []string{}
	for _, r := range d.Requirements {
		kinds = append(kinds, r.Kind)
	}
	slices.Sort(kinds)
	return strings.Join([]string{d.Outcome, d.CarrierID, strings.Join(doctors, ","), strings.Join(kinds, ","), fmt.Sprint(d.SelfPay)}, "|")
}

func joinWithOr(items []string) string {
	if len(items) <= 2 {
		return strings.Join(items, " or ")
	}
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}
