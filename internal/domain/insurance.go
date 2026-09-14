package domain

import (
	"strings"
	"unicode"
)

// RoutingRule determines which providers a patient can see based on their insurance.
type RoutingRule string

const (
	RoutingNotAccepted RoutingRule = "not_accepted"
	RoutingBachOnly    RoutingRule = "bach_only"
	RoutingBachLicht   RoutingRule = "bach_licht"
	RoutingAll         RoutingRule = "all_three"
	RoutingOpticalOnly RoutingRule = "optical_only"
)

// InsuranceEntry maps an insurance name to its AMD carrier ID and routing rule.
type InsuranceEntry struct {
	CarrierID       string
	Routing         RoutingRule
	PreauthRequired bool
}

// insuranceNameMap maps LLM-provided insurance names to carrier ID + routing.
// Keys are normalized (lowercase, no punctuation) via NormalizeForLookup.
// Grouped by carrier ID so you can see which plans share a network.
func lookupInsuranceEntry(name string, entries map[string]InsuranceEntry, aliases map[string]string) (InsuranceEntry, string, bool) {
	normalized := NormalizeForLookup(name)

	if entry, ok := entries[normalized]; ok {
		return entry, normalized, ok
	}

	if canonical, ok := aliases[normalized]; ok {
		entry, ok := entries[canonical]
		return entry, canonical, ok
	}

	return InsuranceEntry{}, "", false
}

func lookupInsuranceFromMaps(name string, entries map[string]InsuranceEntry, aliases map[string]string) (InsuranceEntry, bool) {
	entry, _, ok := lookupInsuranceEntry(name, entries, aliases)
	return entry, ok
}

func lookupVisionInsurance(name string) (InsuranceEntry, bool) {
	if isAetnaGovernmentVisionPlan(name) {
		return visionInsuranceNameMap["icare"], true
	}
	return lookupInsuranceFromMaps(name, visionInsuranceNameMap, visionInsuranceAliases)
}

func isAetnaGovernmentVisionPlan(name string) bool {
	words := strings.FieldsFunc(NormalizeForLookup(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	hasAetna := false
	hasGovernmentProgram := false
	for _, word := range words {
		switch word {
		case "aetna":
			hasAetna = true
		case "medicaid", "medicare":
			hasGovernmentProgram = true
		}
	}
	return hasAetna && hasGovernmentProgram
}

// LookupInsurance looks up an insurance name and returns its entry.
// First tries exact match in insuranceNameMap, then checks insuranceAliases.
// Uses NormalizeForLookup for tolerance of punctuation, casing, and spacing.
func LookupInsurance(name string) (InsuranceEntry, bool) {
	return lookupInsuranceFromMaps(name, insuranceNameMap, insuranceAliases)
}

// IsSelfPayInsurance reports whether a caller-facing insurance value means self-pay.
func IsSelfPayInsurance(name string) bool {
	normalized := NormalizeForLookup(name)
	return normalized == "self pay" ||
		insuranceAliases[normalized] == "self pay" ||
		visionInsuranceAliases[normalized] == "self pay"
}

// LookupInsuranceForCoverage chooses the medical or routine-vision crosswalk.
func LookupInsuranceForCoverage(name string, mode InsuranceMode) (InsuranceEntry, bool) {
	if mode == InsuranceModeVision {
		return lookupVisionInsurance(name)
	}
	return LookupInsurance(name)
}

var crystalRiverRejectedMedicalPlans = map[string]bool{
	// Crystal River inherits the Spring Hill medical rejection list from
	// insuranceNameMap and adds the plans/families below.
	"aetna better health":            true,
	"aetna better health of florida": true,
	"aetna healthy kids":             true,
	"ambetter":                       true,
	"ambetter premier":               true,
	"ambetter select":                true,
	"ambetter value":                 true,
	"community care plan":            true,
	"florida community care":         true,
	"florida complete care":          true,
	"florida medicaid":               true,
	"humana healthy horizons":        true,
	"medicaid":                       true,
	"molina medicaid":                true,
	"simply medicaid":                true,
	"staywell medicare":              true,
	"sunshine medicaid":              true,
	"vivida":                         true,
}

var crystalRiverRejectedCarrierIDs = map[string]bool{
	"car281245": true, // Ambetter / Staywell / Sunshine family
	"car303033": true, // Medicaid / Humana Medicaid legacy bucket
	"car40899":  true, // Florida Medicaid
	"car40907":  true, // iCare / Medicaid family
	"car40912":  true, // Molina Medicaid
}

var ambiguousDemographicCarrierNames = map[string]bool{
	"aetna":                  true,
	"bcbs":                   true,
	"blue cross":             true,
	"blue cross blue shield": true,
	"cigna":                  true,
	"florida blue":           true,
	"humana":                 true,
	"molina":                 true,
	"uhc":                    true,
	"united":                 true,
	"united health care":     true,
	"united healthcare":      true,
}

func applyOfficeMedicalInsurancePolicy(entry InsuranceEntry, canonicalName string, office *OfficeConfig) InsuranceEntry {
	if office == nil {
		return entry
	}
	if office.ID == "crystal_river" {
		if crystalRiverRejectedMedicalPlans[canonicalName] {
			entry.Routing = RoutingNotAccepted
			entry.PreauthRequired = false
		}
		return entry
	}
	return entry
}

// LookupInsuranceForCoverageAtOffice chooses the medical or routine-vision
// crosswalk and applies office-specific medical acceptance rules.
func LookupInsuranceForCoverageAtOffice(name string, mode InsuranceMode, office *OfficeConfig) (InsuranceEntry, bool) {
	if mode == InsuranceModeVision {
		return lookupVisionInsurance(name)
	}
	entry, status := resolveMedicalInsuranceName(name, office)
	if status != insuranceNameMatched {
		return InsuranceEntry{}, false
	}
	return entry, true
}

type insuranceNameStatus uint8

const (
	insuranceNameUnknown insuranceNameStatus = iota
	insuranceNameMatched
	insuranceNameAmbiguous
	insuranceNameUnsupported
)

// resolveMedicalInsuranceName owns office overrides and alias resolution for
// both registration and existing-patient routing. Carrier fallback is a separate
// decision because shared carrier IDs do not identify a specific plan.
func resolveMedicalInsuranceName(name string, office *OfficeConfig) (InsuranceEntry, insuranceNameStatus) {
	paired := isHollywoodSweetwaterMedicalOffice(office)
	if paired {
		if entry, _, ok := lookupInsuranceEntry(name, hollywoodSweetwaterMedicalInsuranceNameMap, hollywoodSweetwaterMedicalInsuranceAliases); ok {
			return entry, insuranceNameMatched
		}
	}
	entry, canonical, ok := lookupInsuranceEntry(name, insuranceNameMap, insuranceAliases)
	if !ok {
		return InsuranceEntry{}, insuranceNameUnknown
	}
	if !paired {
		return applyOfficeMedicalInsurancePolicy(entry, canonical, office), insuranceNameMatched
	}
	if officeEntry, ok := hollywoodSweetwaterMedicalInsuranceNameMap[canonical]; ok {
		return officeEntry, insuranceNameMatched
	}
	if ambiguousDemographicCarrierNames[NormalizeForLookup(name)] || ambiguousDemographicCarrierNames[canonical] {
		return InsuranceEntry{}, insuranceNameAmbiguous
	}
	if entry.Routing == RoutingNotAccepted {
		return entry, insuranceNameMatched
	}
	return InsuranceEntry{}, insuranceNameUnsupported
}

// InsuranceModeForCoverage converts an agent-supplied coverage type to a middleware insurance mode.
func InsuranceModeForCoverage(coverageType string) InsuranceMode {
	switch strings.ToLower(strings.TrimSpace(coverageType)) {
	case "routine_vision", "optical_only":
		return InsuranceModeVision
	}

	switch NormalizeForLookup(coverageType) {
	case "routine vision", "vision", "optical", "optical only":
		return InsuranceModeVision
	default:
		return InsuranceModeMedical
	}
}

// RoutingForCarrierID returns the routing rule for a carrier ID from demographics.
// Returns the rule and whether the carrier is ambiguous (shared across tiers).
// Unknown carrier IDs default to RoutingAll (most permissive).
func RoutingForCarrierID(carrierID string) (RoutingRule, bool) {
	ambiguous := ambiguousCarriers[carrierID]

	if rule, ok := carrierRoutingMap[carrierID]; ok {
		return rule, ambiguous
	}

	// Unknown or ambiguous carriers default to all three
	return RoutingAll, ambiguous
}

// RoutingForCarrierIDAtOffice applies office-specific medical acceptance rules
// to the demographics carrier-ID fallback used for existing patients.
func RoutingForCarrierIDAtOffice(carrierID string, office *OfficeConfig) (RoutingRule, bool) {
	if office != nil && office.ID == "crystal_river" && crystalRiverRejectedCarrierIDs[carrierID] {
		return RoutingNotAccepted, false
	}
	if isHollywoodSweetwaterMedicalOffice(office) {
		if hollywoodSweetwaterAcceptedMedicalCarrierIDs[carrierID] {
			return RoutingBachOnly, ambiguousCarriers[carrierID]
		}
		return RoutingNotAccepted, false
	}
	return RoutingForCarrierID(carrierID)
}

// RoutingForDemographicInsurance prefers AMD's carrier name when available, then
// falls back to carrier ID. Carrier IDs can represent mixed accepted/rejected plans.
func RoutingForDemographicInsurance(carrierID, carrierName string, office *OfficeConfig) (RoutingRule, bool) {
	if !isHollywoodSweetwaterMedicalOffice(office) && ambiguousCarriers[carrierID] && ambiguousDemographicCarrierNames[NormalizeForLookup(carrierName)] {
		return RoutingForCarrierIDAtOffice(carrierID, office)
	}
	entry, status := resolveMedicalInsuranceName(carrierName, office)
	switch status {
	case insuranceNameMatched:
		return entry.Routing, false
	case insuranceNameUnsupported:
		return RoutingNotAccepted, false
	default:
		return RoutingForCarrierIDAtOffice(carrierID, office)
	}
}

// ParseRoutingRule converts a string back to a typed RoutingRule.
// Used by the availability handler to parse the routing param from the request.
func ParseRoutingRule(s string) RoutingRule {
	switch RoutingRule(s) {
	case RoutingNotAccepted:
		return RoutingNotAccepted
	case RoutingBachOnly:
		return RoutingBachOnly
	case RoutingBachLicht:
		return RoutingBachLicht
	case RoutingAll:
		return RoutingAll
	case RoutingOpticalOnly:
		return RoutingOpticalOnly
	default:
		return RoutingAll
	}
}
