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
	// Medical metadata comes from one catalog; the vision source is unchanged.
	CarrierID    string
	CarrierCode  string
	Routing      RoutingRule
	Requirements []InsuranceRequirement
	Providers    []string
}

var insuranceWords = regexp.MustCompile(`[^a-z0-9]+`)

func insuranceNormalize(s string) string {
	return strings.TrimSpace(insuranceWords.ReplaceAllString(strings.ReplaceAll(strings.ToLower(s), "&", " and "), " "))
}
func insuranceContains(s, term string) bool { return strings.Contains(" "+s+" ", " "+term+" ") }

var participationSources = func() map[string][]participationRule {
	result := medicalRules()
	b, err := insuranceSources.ReadFile("insurance_data/INSURANCE_SPRING_HILL_ROUTINE_VISION.json")
	if err != nil {
		panic(err)
	}
	var doc struct {
		Plans []participationRule `json:"plans"`
	}
	if err = json.Unmarshal(b, &doc); err != nil {
		panic(err)
	}
	result["SPRING_HILL_ROUTINE_VISION"] = doc.Plans
	return result
}()

func requirement(kind, channel string) InsuranceRequirement {
	return InsuranceRequirement{kind, channel, "unverified"}
}

// Preserve the existing vision handling of medical-only identities. This reads
// shared identities; it does not apply medical participation rules to vision.
func medicalIdentityCode(name string) string {
	n := insuranceNormalize(name)
	n = strings.TrimSpace(strings.ReplaceAll(n, " medical ", " "))
	n = strings.TrimSuffix(n, " medical")
	for _, p := range medicalPlans {
		switch p.CarrierCode {
		case "UNI20":
			if p.Name != "United Healthcare Individual Exchange" {
				continue
			}
		case "AARPM", "GOL05", "OX04", "UNIT9", "UHC STU", "BIND1", "UNIT15", "PRE04", "HUM02":
		default:
			continue
		}
		for _, alias := range append([]string{p.Name, p.CarrierCode}, p.Aliases...) {
			if n == insuranceNormalize(alias) {
				return p.CarrierCode
			}
		}
	}
	return ""
}

// DecideInsurance accepts a mapped plan for registration. It does
// not run eligibility or verify referrals. No caller boolean can mark one verified.
func DecideInsurance(plan, coverage string, office *OfficeConfig, dob string) InsuranceDecision {
	d := InsuranceDecision{Outcome: "needs_clarification", Participation: "unknown", CoverageType: coverage, OfficeID: office.ID, AllowedProviders: []string{}, Requirements: []InsuranceRequirement{}, Eligibility: "not_checked", Answer: "needs_input: What insurance plan is listed on your card?"}
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
	if coverage == "routine_vision" {
		code := medicalIdentityCode(plan)
		if code == "" && r != nil {
			code = medicalIdentityCode(r.Canonical)
		}
		if code != "" {
			if code == "PRE04" {
				d.Outcome = "not_accepted"
				d.Participation = "not_accepted"
				d.Answer = "blocked: Preferred Care Partners is medical only."
			}
			return d
		}
	}
	if r == nil {
		return d
	}
	if r.Status == "needs_clarification" {
		if r.Clarification != "" {
			d.Answer = "needs_input: " + r.Clarification
		}
		return d
	}
	if r.Status == "not_accepted" {
		d.Outcome = "not_accepted"
		d.Participation = "not_accepted"
		d.Answer = "blocked: This plan is not accepted for this visit type at this office."
		return d
	}
	if r.Status == "needs_staff_task" {
		d.Outcome = "needs_staff_task"
		d.Answer = "blocked: The office needs to confirm this coverage."
		if r.Notice != "" {
			d.Answer += " " + r.Notice
		}
		return d
	}
	d.CanonicalPlan = r.Canonical
	if d.CanonicalPlan == "" {
		d.CanonicalPlan = r.Display
	}
	var entry InsuranceEntry
	var ok bool
	if coverage == "medical" {
		entry = InsuranceEntry{CarrierID: r.CarrierID, Routing: r.Routing}
		ok = r.Routing != ""
		d.CarrierCode = r.CarrierCode
		d.Requirements = append([]InsuranceRequirement{}, r.Requirements...)
		d.CredentialedProviders = r.Providers
	} else {
		entry, ok = lookupVisionInsurance(d.CanonicalPlan)
		if entry.PreauthRequired || r.Preauth {
			d.Requirements = append(d.Requirements, requirement("prior_authorization", ""))
		}
	}
	d.CarrierID = entry.CarrierID
	d.Routing = entry.Routing
	d.SelfPay = IsSelfPayInsurance(d.CanonicalPlan)

	d.Participation = "accepted"
	d.Outcome = "accepted"
	if ok {
		d.Routing = policy.SchedulingRouting(d.Routing, dob)
		d.AllowedProviders = append([]string{}, policy.ProviderNames(d.Routing, dob)...)
	}
	if len(d.CredentialedProviders) > 0 {
		// Intersection: credentialing never grants an office a new provider column.
		allowed := []string{}
		for _, p := range d.AllowedProviders {
			for _, a := range d.CredentialedProviders {
				if p == a || p == "Dr. Bach" && a == "Dr. Austin Bach" {
					allowed = append(allowed, p)
					break
				}
			}
		}
		d.AllowedProviders = allowed
	}
	d.CanSchedule = len(d.Requirements) == 0 && len(d.AllowedProviders) > 0
	d.Answer = "success: Yes, we accept " + d.CanonicalPlan + "."
	if coverage == "routine_vision" {
		d.Answer = "success: Yes, we take " + strings.TrimSpace(plan) + "."
	}
	if len(d.Requirements) > 0 {
		d.Outcome = "needs_staff_task"
		d.Answer = "blocked: This plan requires prior authorization before scheduling."
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
	// AMD stores a carrier directory label, not the patient's exact product.
	// A card-confirmed product may refine that known label, but must still match
	// the chart carrier ID below. An explicit chart product remains authoritative.
	if coverage == "medical" && plan != "" {
		carrierName := medicalCatalog.CarrierNames[chart.CarrierID]
		if carrierName != "" && insuranceNormalize(chart.CarrierName) == insuranceNormalize(carrierName) {
			decision = DecideInsurance(plan, coverage, office, dob)
		}
	}
	// A caller cannot clear a restriction already established by the chart.
	if !decision.CanSchedule {
		if decision.Outcome == "accepted" {
			decision.Outcome = "needs_staff_task"
			decision.Answer = "blocked: Staff must verify the insurance on the chart before scheduling."
		}
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
