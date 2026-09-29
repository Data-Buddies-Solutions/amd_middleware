package insurance

import "strings"

type participationMatchCandidate struct {
	rule     *participationRule
	term     string
	name     string
	required bool
}

func participationMatch(source, query string) *participationRule {
	query = insuranceNormalize(query)
	var matches []participationMatchCandidate
	for i := range participationSources[source] {
		rule := &participationSources[source][i]
		display := rule.Display
		if display == "" {
			display = rule.Canonical
		}
		for _, name := range append([]string{display}, rule.Aliases...) {
			term := insuranceNormalize(name)
			if term != "" && insuranceContains(query, term) {
				matches = append(matches, participationMatchCandidate{rule: rule, term: term, name: name})
			}
		}
		for _, name := range rule.RequiredAliases {
			term := insuranceNormalize(name)
			if insuranceContainsWords(query, term) {
				matches = append(matches, participationMatchCandidate{rule: rule, term: term, name: name, required: true})
			}
		}
	}

	var selected *participationMatchCandidate
	for _, match := range matches {
		moreSpecific := false
		for _, other := range matches {
			if len(other.term) <= len(match.term) {
				continue
			}
			if insuranceContains(other.term, match.term) ||
				((other.required || match.required) && insuranceContainsWords(other.term, match.term)) {
				moreSpecific = true
				break
			}
		}
		if moreSpecific {
			continue
		}
		if selected != nil && (selected.rule.Canonical != match.rule.Canonical || selected.rule.Status != match.rule.Status) {
			if source == "SPRING_HILL_ROUTINE_VISION" && equivalentVisionAlias(*selected, match) {
				continue
			}
			return nil
		}
		selected = &match
	}
	if selected == nil {
		return nil
	}
	if source == "SPRING_HILL_ROUTINE_VISION" {
		resolved := *selected.rule
		resolved.Display = selected.name
		return &resolved
	}
	return selected.rule
}

func equivalentVisionAlias(a, b participationMatchCandidate) bool {
	if a.term != b.term || a.rule.Status != b.rule.Status || a.rule.Preauth != b.rule.Preauth ||
		a.rule.Notice != b.rule.Notice || a.rule.Clarification != b.rule.Clarification {
		return false
	}
	left, leftOK := lookupVisionInsurance(a.rule.Canonical)
	right, rightOK := lookupVisionInsurance(b.rule.Canonical)
	return leftOK && rightOK && left.CarrierID != "" && left == right
}

func insuranceContainsWords(query, term string) bool {
	if term == "" {
		return false
	}
	for _, word := range strings.Fields(term) {
		if !insuranceContains(query, word) {
			return false
		}
	}
	return true
}
