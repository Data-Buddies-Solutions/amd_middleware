package insurance

import (
	"slices"
	"time"

	"advancedmd-token-management/internal/domain"
)

func DecideOfferedAnswer(heard string, offeredPlanIDs []string, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	d, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	list := listForOffice(office.ID)
	offered := []plan{}
	for _, p := range list.Plans {
		if p.Coverage == coverage && slices.Contains(offeredPlanIDs, p.ID) {
			offered = append(offered, p)
		}
	}
	if len(offered) == 0 {
		return DecideInsurance(heard, coverage, office, dob, asOf)
	}
	words := callerWords(tokens(heard), list.vocabulary[coverage])
	if len(words) == 0 {
		return decideCandidates(d, offered, office, dob, asOf)
	}
	for _, p := range offered {
		if p.hasName(words) {
			return decidePlanAtOffice(p, office, dob, asOf)
		}
	}
	named := slices.DeleteFunc(slices.Clone(offered), func(p plan) bool { return !labelHasEvery(p, words) })
	if len(named) == 0 {
		return DecideInsurance(heard, coverage, office, dob, asOf)
	}
	return decideCandidates(d, named, office, dob, asOf)
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
