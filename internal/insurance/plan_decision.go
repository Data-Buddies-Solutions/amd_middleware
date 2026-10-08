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
	d.CallerNotice = p.CallerNotice
	for _, r := range p.Requirements {
		d.Requirements = append(d.Requirements, InsuranceRequirement{Kind: r.Kind, Channel: r.Channel, Verification: "unverified"})
	}
	if len(p.OnlyOffices) > 0 && !slices.Contains(p.OnlyOffices, office.ID) {
		return notAccepted(d)
	}
	accepting, pending := acceptingDoctors(p, office)
	if len(accepting) == 0 {
		if pending {
			d.Outcome = "needs_staff_task"
			d.Reason = "pending_confirmation"
			d.Answer = withNotice(answerConfirm, d.CallerNotice)
			return d
		}
		return notAccepted(d)
	}
	d.Participation = "accepted"
	d.Outcome = "needs_staff_task"
	d.AllowedProviders = doctorsForPatient(accepting, p.Coverage, office, dob)
	if len(d.AllowedProviders) == 0 {
		d.Reason = "no_provider_for_age"
		d.Answer = withNotice(answerNoDoctorForAge, d.CallerNotice)
		return d
	}
	if len(p.Requirements) > 0 {
		d.Reason = "requirement"
		d.Answer = withNotice(requirementAnswer(p.Requirements[0].Kind), d.CallerNotice)
		return d
	}
	d.Outcome = "accepted"
	d.CanSchedule = true
	d.Reason = "accepted"
	d.Answer = withNotice("success: Yes, we accept "+p.Label+".", d.CallerNotice)
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

func notAccepted(d InsuranceDecision) InsuranceDecision {
	d.Outcome = "not_accepted"
	d.Participation = "not_accepted"
	d.Reason = "not_accepted"
	d.Answer = withNotice(answerNotAccepted, d.CallerNotice)
	return d
}

func requirementAnswer(kind string) string {
	switch kind {
	case "pcp_referral":
		return answerReferral
	case "staff_verify":
		return answerStaffVerify
	}
	return answerPriorAuth
}

func withNotice(answer, notice string) string {
	if notice == "" {
		return answer
	}
	return answer + " " + notice
}
