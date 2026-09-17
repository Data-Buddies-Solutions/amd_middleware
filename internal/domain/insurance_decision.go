package domain

import (
	"embed"
	"encoding/json"
	"regexp"
	"strings"
)

// These participation sources moved from Python. Provider transport IDs remain
// private; a billing code is not an AdvancedMD record ID.
//
//go:embed insurance_data/*.json
var insuranceSources embed.FS

type InsuranceRequirement struct {
	Kind         string `json:"kind"`
	Channel      string `json:"channel,omitempty"`
	Verification string `json:"verification"`
}

type InsuranceDecision struct {
	Outcome               string                 `json:"outcome"`
	Participation         string                 `json:"participation"`
	CanonicalPlan         string                 `json:"canonicalPlan,omitempty"`
	CarrierCode           string                 `json:"carrierCode,omitempty"`
	CoverageType          string                 `json:"coverageType"`
	OfficeID              string                 `json:"officeId"`
	Routing               RoutingRule            `json:"routing,omitempty"`
	AllowedProviders      []string               `json:"allowedProviders"`
	CredentialedProviders []string               `json:"credentialedProviders,omitempty"`
	Requirements          []InsuranceRequirement `json:"requirements"`
	Eligibility           string                 `json:"eligibility"`
	CanRegister           bool                   `json:"canRegister"`
	CanSchedule           bool                   `json:"canSchedule"`
	SelfPay               bool                   `json:"selfPay"`
	Answer                string                 `json:"answer"`
	CarrierID             string                 `json:"-"`
}

type participationRule struct {
	Status          string   `json:"status"`
	Canonical       string   `json:"canonicalPlan"`
	Display         string   `json:"displayName"`
	Aliases         []string `json:"aliases"`
	RequiredAliases []string `json:"requiredWordAliases"`
	Notice          string   `json:"callerNotice"`
	Clarification   string   `json:"clarificationNeeded"`
	Preauth         bool     `json:"preauthRequired"`
}

var insuranceWords = regexp.MustCompile(`[^a-z0-9]+`)

func insuranceNormalize(s string) string {
	return strings.TrimSpace(insuranceWords.ReplaceAllString(strings.ReplaceAll(strings.ToLower(s), "&", " and "), " "))
}
func insuranceContains(s, term string) bool { return strings.Contains(" "+s+" ", " "+term+" ") }

var participationSources = func() map[string][]participationRule {
	result := map[string][]participationRule{}
	entries, err := insuranceSources.ReadDir("insurance_data")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		b, err := insuranceSources.ReadFile("insurance_data/" + e.Name())
		if err != nil {
			panic(err)
		}
		var doc struct {
			Plans []participationRule `json:"plans"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			panic(err)
		}
		// Feed every known product name into the same ambiguity-aware matcher.
		for i := range doc.Plans {
			rule := &doc.Plans[i]
			if corrected := correctedInsurance(rule.Canonical); corrected != nil {
				rule.Canonical = corrected.name
				rule.Aliases = append(rule.Aliases, corrected.name, corrected.code)
				for alias, canonical := range correctedAliases {
					if canonical == corrected.name {
						rule.Aliases = append(rule.Aliases, alias)
					}
				}
			}
		}
		result[strings.TrimSuffix(strings.TrimPrefix(e.Name(), "INSURANCE_"), ".json")] = doc.Plans
	}
	return result
}()

// Corrected plan identities are exact, not substring aliases to a parent carrier.
// Empty IDs intentionally fail closed until a practice carrier export verifies them.
type correctedPlan struct {
	name, code, id string
	requirements   []InsuranceRequirement
	providers      []string
}

func requirement(kind, channel string) InsuranceRequirement {
	return InsuranceRequirement{kind, channel, "unverified"}
}

var correctedPlans = []correctedPlan{
	{"United Healthcare Individual Exchange", "UNI20", "", []InsuranceRequirement{requirement("pcp_referral", "uhc_portal")}, nil},
	{"United Healthcare AARP Medicare", "AARPM", "", nil, nil},
	{"United Healthcare Golden Rule", "GOL05", "", nil, nil},
	{"United Healthcare Oxford", "OX04", "", nil, nil},
	{"United Healthcare Shared Services", "UNIT9", "", nil, nil},
	{"United Healthcare Student Resources", "UHC STU", "", nil, nil},
	{"United Healthcare Surest", "BIND1", "", nil, nil},
	{"United Healthcare Global", "UNIT15", "", []InsuranceRequirement{requirement("vob_authorization", "")}, nil},
	{"Preferred Care Partners", "PRE04", "car40916", nil, []string{"Dr. Austin Bach", "Dr. Calero", "Dr. Casas"}},
	{"Humana Medicaid HMO", "HUM02", "", []InsuranceRequirement{requirement("prior_authorization", "availity")}, nil},
}
var correctedAliases = map[string]string{
	"united individual exchange": "United Healthcare Individual Exchange", "united healthcare individual exchange network": "United Healthcare Individual Exchange",
	"united aarp medicare complete": "United Healthcare AARP Medicare", "united aarp medicare complete medicare advantage hmo lppo": "United Healthcare AARP Medicare",
	"united aarp medicare advantage": "United Healthcare AARP Medicare", "united golden rule": "United Healthcare Golden Rule",
	"united oxford": "United Healthcare Oxford", "united shared services": "United Healthcare Shared Services", "united student resources": "United Healthcare Student Resources",
	"united surest": "United Healthcare Surest", "united healthcare surest health plans formerly bind": "United Healthcare Surest",
	"united global international plan": "United Healthcare Global", "united healthcare global international plan": "United Healthcare Global",
	"preferred care network preferred care partners": "Preferred Care Partners", "preferred care network": "Preferred Care Partners", "preferred care": "Preferred Care Partners",
	"humana medicaid": "Humana Medicaid HMO",
}

func correctedInsurance(name string) *correctedPlan {
	n := insuranceNormalize(name)
	n = strings.TrimSpace(strings.ReplaceAll(n, " medical ", " "))
	n = strings.TrimSuffix(n, " medical")
	if a, ok := correctedAliases[n]; ok {
		n = insuranceNormalize(a)
	}
	for i := range correctedPlans {
		p := &correctedPlans[i]
		if n == insuranceNormalize(p.name) || n == insuranceNormalize(p.code) {
			return p
		}
	}
	return nil
}

// DecideInsurance is the sole participation/write/scheduling decision. It does
// not run eligibility or verify referrals. No caller boolean can mark one verified.
func DecideInsurance(plan, coverage string, office *OfficeConfig, dob string) InsuranceDecision {
	d := InsuranceDecision{Outcome: "needs_clarification", Participation: "unknown", CoverageType: coverage, OfficeID: office.ID, AllowedProviders: []string{}, Requirements: []InsuranceRequirement{}, Eligibility: "not_checked", Answer: "needs_input: Ask for the exact plan name from the insurance card."}
	if coverage != "medical" && coverage != "routine_vision" {
		d.Answer = "needs_input: Specify medical or routine vision coverage."
		return d
	}
	source := "SPRING_HILL_MEDICAL"
	switch office.ID {
	case "hollywood", "sweetwater":
		source = "HOLLYWOOD_SWEETWATER"
	case "crystal_river":
		source = "CRYSTAL_RIVER"
	}
	policy := NewSchedulingPolicy(office)
	if coverage == "routine_vision" {
		source = "SPRING_HILL_ROUTINE_VISION"
	}
	if (coverage == "medical" && !policy.SupportsMedical()) || (coverage == "routine_vision" && !policy.SupportsRouting(RoutingOpticalOnly)) {
		d.Outcome = "not_accepted"
		d.Participation = "not_accepted"
		d.Answer = "blocked: This office does not accept coverage for that visit type."
		return d
	}
	r := participationMatch(source, plan)
	correction := correctedInsurance(plan)
	if r != nil && correction == nil {
		correction = correctedInsurance(r.Canonical)
	}
	if correction != nil && coverage == "routine_vision" {
		if correction.code == "PRE04" {
			d.Outcome = "not_accepted"
			d.Participation = "not_accepted"
			d.Answer = "blocked: Preferred Care Partners is medical only."
		}
		return d
	}
	if correction != nil && coverage == "medical" {
		d.CanonicalPlan = correction.name
		d.CarrierCode = correction.code
		d.Requirements = append(d.Requirements, correction.requirements...)
		d.CredentialedProviders = correction.providers
		// Preserve explicit office exclusions; the group PDF is not a blanket office contract.
		r = participationMatch(source, correction.name)
		if r == nil || (correctedInsurance(r.Canonical) == nil && insuranceNormalize(r.Canonical) != insuranceNormalize(correction.name)) {
			d.Answer = "blocked: Staff must confirm this plan's participation at this office."
			d.Outcome = "needs_staff_task"
			return d
		}

	}
	if r == nil {
		return d
	}
	if correction == nil && insuranceNormalize(r.Canonical) == "united healthcare" {
		return d
	}
	if r.Status == "needs_clarification" {
		if r.Clarification != "" {
			d.Answer = "needs_input: Ask for " + r.Clarification
		}
		return d
	}
	if r.Status == "not_accepted" {
		d.Outcome = "not_accepted"
		d.Participation = "not_accepted"
		d.Answer = "blocked: This plan is not accepted for this visit type at this office."
		return d
	}
	if reason := insuranceSourceConflict(plan, r.Canonical, coverage, office.ID); reason != "" {
		d.Outcome = "needs_staff_task"
		d.Answer = "blocked: " + reason + " Staff must confirm participation before proceeding."
		return d
	}
	d.CanonicalPlan = r.Canonical
	if d.CanonicalPlan == "" {
		d.CanonicalPlan = r.Display
	}
	entry, ok := LookupInsuranceForCoverageAtOffice(d.CanonicalPlan, InsuranceModeForCoverage(coverage), office)
	d.CarrierID = entry.CarrierID
	d.Routing = entry.Routing
	if correction != nil && coverage == "medical" {
		d.CanonicalPlan = correction.name
		d.CarrierCode = correction.code
		d.CarrierID = correction.id
		if correction.code == "PRE04" {
			d.Routing = RoutingBachOnly
			ok = true
		}
	} else if entry.PreauthRequired || r.Preauth {
		d.Requirements = append(d.Requirements, requirement("prior_authorization", ""))
	}
	if coverage == "medical" && insuranceNormalize(d.CanonicalPlan) == "united healthcare nhp hmo only" {
		d.Requirements = append(d.Requirements, requirement("pcp_referral", ""))
	}
	if coverage == "medical" {
		switch insuranceNormalize(d.CanonicalPlan) {
		case "ambetter value", "molina medicare":
			d.Requirements = append(d.Requirements, requirement("pcp_referral", ""))
		case "umr", "aetna epo university of miami", "aetna epo north broward":
			d.Requirements = append(d.Requirements, requirement("network_review", ""))
		}
	}
	d.SelfPay = IsSelfPayInsurance(d.CanonicalPlan)

	if correction == nil && (!ok || entry.Routing == RoutingNotAccepted) && r.Status == "accepted" {
		d.Outcome = "needs_staff_task"
		d.Answer = "blocked: Insurance sources conflict for this office and visit type. Ask staff to confirm participation."
		return d
	}
	d.Participation = "accepted"
	d.Outcome = "accepted"
	d.Routing = policy.SchedulingRouting(d.Routing, dob)
	d.AllowedProviders = append([]string{}, policy.ProviderNames(d.Routing, dob)...)
	if correction != nil && len(correction.providers) > 0 {
		// Intersection: credentialing never grants an office a new provider column.
		allowed := []string{}
		for _, p := range d.AllowedProviders {
			for _, a := range correction.providers {
				if p == a || p == "Dr. Bach" && a == "Dr. Austin Bach" {
					allowed = append(allowed, p)
					break
				}
			}
		}
		d.AllowedProviders = allowed
	}
	d.CanRegister = ok && d.CarrierID != "" && d.Routing != RoutingNotAccepted
	d.CanSchedule = d.CanRegister && len(d.Requirements) == 0 && len(d.AllowedProviders) > 0 && r.Status == "accepted"
	prefix := "success: "
	if !d.CanRegister || len(d.Requirements) > 0 || r.Status == "needs_staff_task" {
		d.Outcome = "needs_staff_task"
		prefix = "blocked: "
	}
	d.Answer = prefix + "This office participates with " + d.CanonicalPlan + " for this visit type. This does not verify active coverage or benefits."
	if len(d.Requirements) > 0 {
		d.Answer += " Staff must complete the required insurance review before scheduling."
	}
	for _, req := range d.Requirements {
		switch req.Kind {
		case "pcp_referral":
			if req.Channel == "uhc_portal" {
				d.Answer += " A PCP referral must be recorded in the insurer's portal."
			} else {
				d.Answer += " A PCP referral is required."
			}
		case "prior_authorization":
			d.Answer += " Prior authorization is required."
		case "network_review":
			d.Answer += " The office must confirm network limitations and any required authorization."
		case "vob_authorization":
			d.Answer += " Verification of Benefits authorization is required."
		}
	}
	if !d.CanRegister {
		d.Answer += " Staff must verify the insurance attachment details before registration or insurance changes."
	}
	if r.Notice != "" {
		d.Answer += " " + r.Notice
	}
	return d
}

// DecideChartInsurance binds a caller's clarified product to the carrier actually
// on the chart. It never silently changes that chart or trusts request routing.
func DecideChartInsurance(chart PatientDemographics, plan, coverage string, office *OfficeConfig, dob string) InsuranceDecision {
	recordedPlan := chart.CarrierName
	if chart.CarrierID == "car40916" {
		recordedPlan = "Preferred Care Partners"
	}
	decision := DecideInsurance(recordedPlan, coverage, office, dob)
	// A caller cannot clear a restriction already established by the chart.
	if !decision.CanSchedule {
		return decision
	}
	matches := chart.CarrierID != "" && chart.CarrierID == decision.CarrierID
	if plan != "" {
		claimed := DecideInsurance(plan, coverage, office, dob)
		matches = matches && claimed.CanSchedule && insuranceNormalize(claimed.CanonicalPlan) == insuranceNormalize(decision.CanonicalPlan)
	}
	if !matches {
		decision.CanSchedule = false
		decision.Outcome = "needs_staff_task"
		decision.Answer = "blocked: Staff must verify the insurance on the chart before scheduling."
	}
	return decision
}

// correctedEntry keeps the legacy lookup helpers on the same carrier identity
// as the decision contract. Never substitute a parent carrier for a missing ID.
func correctedEntry(code string, routing RoutingRule) InsuranceEntry {
	for _, p := range correctedPlans {
		if p.code == code {
			return InsuranceEntry{CarrierID: p.id, Routing: routing, PreauthRequired: len(p.requirements) > 0}
		}
	}
	panic("unknown corrected insurance code")
}

// The group PDF (7/7/2026) conflicts with some older office lists. Preserve the
// conflict as a review hold; never expand an office contract by inference.
func insuranceSourceConflict(plan, canonical, coverage, office string) string {
	n := insuranceNormalize(plan + " " + canonical)
	if office == "hollywood" && (strings.Contains(n, "aetna better health") || strings.Contains(n, "molina medicaid")) {
		return "The reference limits this Medicaid plan to Miami-Dade, but the older office list includes it here."
	}
	if office == "sweetwater" && (strings.Contains(n, "freedom") || strings.Contains(n, "optimum") || (coverage == "medical" && (strings.Contains(n, "careplus") || strings.Contains(n, "care plus")))) {
		return "The reference limits this plan to other offices, but the older office list includes it here."
	}
	if coverage == "routine_vision" && (strings.Contains(n, "careplus") || strings.Contains(n, "care plus")) {
		return "Routine-vision credentialing is listed as pending in the reference."
	}
	if coverage == "medical" && (insuranceNormalize(canonical) == "multiplan phcs" || insuranceNormalize(canonical) == "imagine health" || insuranceNormalize(canonical) == "aetna epo") {
		return "The exact underlying plan and network limitations need confirmation."
	}
	return ""
}
