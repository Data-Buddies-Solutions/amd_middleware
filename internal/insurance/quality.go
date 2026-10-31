package insurance

import (
	"time"

	"advancedmd-token-management/internal/domain"
)

var contradictedPhrasings = []struct {
	heard, coverage, planID string
}{
	{"Staywell Medicaid", "medical", "staywell-medicare-medical"},
	{"Sunshine Medicare", "medical", "sunshine-medicaid-medical"},
	{"Oscar Medicare", "medical", "oscar-health-medical"},
	{"United Healthcare Medicare", "routine_vision", "unitedhealthcare-vision"},
	{"Cigna Medicare Advantage HMO", "routine_vision", "cigna-vision"},
	{"Ambetter Premier", "routine_vision", "ambetter-vision"},
	{"Humana Gold PPO", "medical", "humana-medicare-hmo"},
	{"BCBS HMO", "routine_vision", "florida-blue-medicare-vision"},
	{"Molina Medicade", "medical", "molina-medicare-medical"},
	{"Medicare Supplement", "medical", "tricare-for-life"},
}

func (c *catalogCheck) decisions(lists []planList) {
	for _, list := range lists {
		for _, officeID := range list.Offices {
			office, ok := domain.LookupOfficeInEnvironment(domain.Prod, officeID)
			if !ok {
				continue
			}
			c.namesResolve(list, office)
			c.selfPayAccepted(list, office)
			c.phrasingsAvoidContradictedPlans(list, office)
		}
	}
}

func (c *catalogCheck) namesResolve(list planList, office *domain.OfficeConfig) {
	for _, p := range list.Plans {
		if !officeSupports(office, p.Coverage) {
			continue
		}
		byID := decidePlan(list, p.ID, p.Coverage, office, "", time.Time{})
		for _, name := range planNames(p) {
			if byName := decideInsurance(list, name, p.Coverage, office, "", time.Time{}); byName.PlanID != p.ID || byName.Outcome != byID.Outcome {
				c.fail(list.Region, list.Table, p.ID, ruleNameDoesNotResolve, "at %s, %q decides %s for plan %q instead of %s for this plan", office.ID, name, byName.Outcome, byName.PlanID, byID.Outcome)
			}
		}
	}
}

func (c *catalogCheck) selfPayAccepted(list planList, office *domain.OfficeConfig) {
	for _, coverage := range []string{"medical", "routine_vision"} {
		if !officeSupports(office, coverage) {
			continue
		}
		if d := decideSelfPay(list, coverage, office, "", time.Time{}); d.Outcome != "accepted" || !d.CanSchedule || !d.SelfPay {
			c.fail(list.Region, list.Table, d.PlanID, ruleNoAcceptedSelfPay, "at %s, self-pay for %s decides %s (%s), not accepted", office.ID, coverage, d.Outcome, d.Reason)
		}
	}
}

func (c *catalogCheck) phrasingsAvoidContradictedPlans(list planList, office *domain.OfficeConfig) {
	for _, phrasing := range contradictedPhrasings {
		if !officeSupports(office, phrasing.coverage) {
			continue
		}
		if d := decideInsurance(list, phrasing.heard, phrasing.coverage, office, "", time.Time{}); d.PlanID == phrasing.planID {
			c.fail(list.Region, list.Table, d.PlanID, ruleContradictedPhrasing, "at %s, %q decides this plan, which the caller's words contradict", office.ID, phrasing.heard)
		}
	}
}
