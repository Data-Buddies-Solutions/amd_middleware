package insurance

import (
	"slices"

	"advancedmd-token-management/internal/domain"
)

func DecideOfferedAnswer(heard string, offeredPlanIDs []string, coverage string, office *domain.OfficeConfig, dob string) InsuranceDecision {
	d, list, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	offered := []plan{}
	for _, p := range list.Plans {
		if p.Coverage == coverage && slices.Contains(offeredPlanIDs, p.ID) {
			offered = append(offered, p)
		}
	}
	if len(offered) == 0 {
		return DecideInsurance(heard, coverage, office, dob)
	}
	words := callerWords(tokens(heard), list.vocabulary[coverage])
	if len(words) == 0 {
		return decideCandidates(d, offered, office, dob)
	}
	for _, p := range offered {
		if p.hasName(words) {
			return decidePlanAtOffice(p, office, dob)
		}
	}
	named := slices.DeleteFunc(slices.Clone(offered), func(p plan) bool { return !labelHasEvery(p, words) })
	if len(named) == 0 {
		return DecideInsurance(heard, coverage, office, dob)
	}
	return decideCandidates(d, named, office, dob)
}

func labelHasEvery(p plan, words []string) bool {
	label := tokens(p.Label)
	for _, word := range words {
		if !slices.ContainsFunc(label, func(l string) bool { return tokenPoints(word, l) > 0 }) {
			return false
		}
	}
	return true
}
