package insurance

import (
	"slices"

	"advancedmd-token-management/internal/domain"
)

func decidePlanAtOffice(p plan, office *domain.OfficeConfig, dob string) InsuranceDecision {
	d := newDecision(p.Coverage, office)
	d.PlanID = p.ID
	d.CanonicalPlan = p.Label
	d.CarrierCode = p.CarrierCode
	d.CarrierID = p.CarrierID
	d.SelfPay = p.SelfPay
	for _, r := range p.Requirements {
		d.Requirements = append(d.Requirements, InsuranceRequirement{Kind: r.Kind, Channel: r.Channel, Verification: "unverified"})
	}
	if len(p.OnlyOffices) > 0 && !slices.Contains(p.OnlyOffices, office.ID) {
		return notAccepted(d, p.CallerNotice)
	}
	accepting, pending := acceptingDoctors(p, office)
	if len(accepting) == 0 {
		if pending {
			d.Outcome = "needs_staff_task"
			d.Answer = withNotice(answerConfirm, p.CallerNotice)
			return d
		}
		return notAccepted(d, p.CallerNotice)
	}
	d.Participation = "accepted"
	d.Outcome = "needs_staff_task"
	d.AllowedProviders = doctorsForPatient(accepting, p.Coverage, office, dob)
	if len(d.AllowedProviders) == 0 {
		d.Answer = withNotice(answerNoDoctorForAge, p.CallerNotice)
		return d
	}
	if len(p.Requirements) > 0 {
		d.Answer = withNotice(requirementAnswer(p.Requirements[0].Kind), p.CallerNotice)
		return d
	}
	d.Outcome = "accepted"
	d.CanSchedule = true
	d.Answer = withNotice("success: Yes, we accept "+p.Label+".", p.CallerNotice)
	return d
}

func visitRouting(coverage string) domain.RoutingRule {
	if coverage == "routine_vision" {
		return domain.RoutingOpticalOnly
	}
	return domain.RoutingAll
}

func acceptingDoctors(p plan, office *domain.OfficeConfig) ([]string, bool) {
	accepting := []string{}
	pending := false
	for _, doctor := range office.ProvidersForRoutingAndDOB(visitRouting(p.Coverage), "") {
		switch p.Doctors[displayName(office, doctor)] {
		case "yes":
			accepting = append(accepting, doctor)
		case "pending":
			pending = true
		}
	}
	return accepting, pending
}

func doctorsForPatient(accepting []string, coverage string, office *domain.OfficeConfig, dob string) []string {
	allowed := []string{}
	for _, doctor := range office.ProvidersForRoutingAndDOB(office.SchedulingRouting(visitRouting(coverage), dob), dob) {
		if slices.Contains(accepting, doctor) {
			allowed = append(allowed, doctor)
		}
	}
	return allowed
}

func displayName(office *domain.OfficeConfig, shortName string) string {
	for _, column := range office.Columns {
		if column.ShortName == shortName {
			return column.DisplayName
		}
	}
	return ""
}

func notAccepted(d InsuranceDecision, notice string) InsuranceDecision {
	d.Outcome = "not_accepted"
	d.Participation = "not_accepted"
	d.Answer = withNotice(answerNotAccepted, notice)
	return d
}

func requirementAnswer(kind string) string {
	if kind == "pcp_referral" {
		return answerReferral
	}
	return answerPriorAuth
}

func withNotice(answer, notice string) string {
	if notice == "" {
		return answer
	}
	return answer + " " + notice
}
