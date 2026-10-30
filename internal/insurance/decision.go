package insurance

import (
	"advancedmd-token-management/internal/domain"
	"time"
)

const (
	answerAskCard          = "needs_input: What insurance plan is listed on your card?"
	answerAskFullName      = "needs_input: What is the full plan name on your card?"
	answerAskCoverage      = "needs_input: Specify medical or routine vision coverage."
	answerOfficeNoCoverage = "blocked: This office does not accept coverage for that visit type."
	answerNotAccepted      = "blocked: This plan is not accepted for this visit type at this office."
	answerConfirm          = "blocked: The office needs to confirm this coverage."
	answerPriorAuth        = "blocked: This plan requires prior authorization before scheduling."
	answerReferral         = "blocked: This plan requires a referral from your primary care doctor before scheduling."
	answerStaffVerify      = "blocked: The office needs to verify this plan's coverage before scheduling."
	answerChartStaff       = "blocked: Staff must verify the insurance on the chart before scheduling."
	answerNoDoctorForAge   = "blocked: We accept this plan, but none of its doctors at this office can see a patient of this age. The office needs to arrange this visit."
	answerNoSelfPay        = "blocked: This office does not offer self-pay for this visit type."
	maxOptions             = 4
)

type InsuranceRequirement struct {
	Kind         string `json:"kind"`
	Channel      string `json:"channel,omitempty"`
	Verification string `json:"verification"`
}

type InsuranceOption struct {
	PlanID string `json:"planId"`
	Label  string `json:"label"`
}

type InsuranceDecision struct {
	Outcome          string                 `json:"outcome"`
	Participation    string                 `json:"participation"`
	PlanID           string                 `json:"planId,omitempty"`
	CanonicalPlan    string                 `json:"canonicalPlan,omitempty"`
	CarrierCode      string                 `json:"carrierCode,omitempty"`
	CarrierID        string                 `json:"carrierId,omitempty"`
	CoverageType     string                 `json:"coverageType"`
	OfficeID         string                 `json:"officeId"`
	AllowedProviders []string               `json:"allowedProviders"`
	Requirements     []InsuranceRequirement `json:"requirements"`
	Eligibility      string                 `json:"eligibility"`
	CanSchedule      bool                   `json:"canSchedule"`
	SelfPay          bool                   `json:"selfPay"`
	Reason           string                 `json:"reason"`
	CallerNotice     string                 `json:"callerNotice,omitempty"`
	Answer           string                 `json:"answer"`
	Options          []InsuranceOption      `json:"options,omitempty"`
}

func DecideInsurance(heard, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	return decideInsurance(listForOffice(office.ID), heard, coverage, office, dob, asOf)
}

func decideInsurance(list planList, heard, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	d, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	heardWords := tokens(heard)
	words := callerWords(heardWords, list.vocabulary[coverage])
	if len(words) < len(heardWords) && namesProgram(words) {
		d.Reason, d.Answer = "ask_full_name", answerAskFullName
		return d
	}
	if p, ok := exactPlan(list, coverage, words); ok {
		return decidePlanAtOffice(p, office, dob, asOf)
	}
	found := bestCandidates(list, coverage, words)
	if len(found) == 0 {
		if namesProgram(words) {
			d.Reason, d.Answer = "ask_full_name", answerAskFullName
		}
		return d
	}
	return decideCandidates(d, found, office, dob, asOf)
}

func DecidePlan(planID, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	return decidePlan(listForOffice(office.ID), planID, coverage, office, dob, asOf)
}

func decidePlan(list planList, planID, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	d, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	for _, p := range list.Plans {
		if p.ID == planID && p.Coverage == coverage {
			return decidePlanAtOffice(p, office, dob, asOf)
		}
	}
	return d
}

func DecideSelfPay(coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	return decideSelfPay(listForOffice(office.ID), coverage, office, dob, asOf)
}

func decideSelfPay(list planList, coverage string, office *domain.OfficeConfig, dob string, asOf time.Time) InsuranceDecision {
	d, ok := startDecision(coverage, office)
	if !ok {
		return d
	}
	for _, p := range list.Plans {
		if p.SelfPay && p.Coverage == coverage {
			return decidePlanAtOffice(p, office, dob, asOf)
		}
	}
	d.Outcome = "not_accepted"
	d.Participation = "not_accepted"
	d.Reason, d.Answer = "no_self_pay", answerNoSelfPay
	return d
}

func startDecision(coverage string, office *domain.OfficeConfig) (InsuranceDecision, bool) {
	d := newDecision(coverage, office)
	if coverage != "medical" && coverage != "routine_vision" {
		d.Reason, d.Answer = "ask_coverage", answerAskCoverage
		return d, false
	}
	if !officeSupports(office, coverage) {
		d.Outcome = "not_accepted"
		d.Participation = "not_accepted"
		d.Reason, d.Answer = "office_no_coverage", answerOfficeNoCoverage
		return d, false
	}
	return d, true
}

func newDecision(coverage string, office *domain.OfficeConfig) InsuranceDecision {
	return InsuranceDecision{
		Outcome:          "needs_clarification",
		Participation:    "unknown",
		CoverageType:     coverage,
		OfficeID:         office.ID,
		AllowedProviders: []string{},
		Requirements:     []InsuranceRequirement{},
		Eligibility:      "not_checked",
		Reason:           "ask_card",
		Answer:           answerAskCard,
	}
}

func officeSupports(office *domain.OfficeConfig, coverage string) bool {
	if coverage == "medical" {
		return office.SupportsMedical()
	}
	return office.SupportsRouting(domain.RoutingOpticalOnly)
}
