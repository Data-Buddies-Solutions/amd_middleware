package domain

import "strings"

type participationMatchCandidate struct {
	rule     *participationRule
	term     string
	required bool
}

// participationMatch accepts a specific product over its parent name, but never
// chooses between different products merely because one name is longer.
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
				matches = append(matches, participationMatchCandidate{rule, term, false})
			}
		}
		for _, name := range rule.RequiredAliases {
			term := insuranceNormalize(name)
			if insuranceContainsWords(query, term) {
				matches = append(matches, participationMatchCandidate{rule, term, true})
			}
		}
	}

	var selected *participationRule
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
		if selected != nil && (selected.Canonical != match.rule.Canonical || selected.Status != match.rule.Status) {
			return nil
		}
		selected = match.rule
	}
	return selected
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
