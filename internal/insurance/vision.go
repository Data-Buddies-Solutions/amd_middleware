package insurance

import (
	"strings"
	"unicode"

	"advancedmd-token-management/internal/domain"
)

type InsuranceEntry struct {
	CarrierID       string
	Routing         domain.RoutingRule
	PreauthRequired bool
}

var VisionInsuranceNameMap = map[string]InsuranceEntry{
	"vsp":                     {CarrierID: "car280695", Routing: domain.RoutingOpticalOnly},
	"eyemed":                  {CarrierID: "car280684", Routing: domain.RoutingOpticalOnly},
	"nva":                     {CarrierID: "car308794", Routing: domain.RoutingOpticalOnly},
	"davis":                   {CarrierID: "car280612", Routing: domain.RoutingOpticalOnly},
	"spectera":                {CarrierID: "car308790", Routing: domain.RoutingOpticalOnly},
	"solstice":                {CarrierID: "car301652", Routing: domain.RoutingOpticalOnly},
	"icare":                   {CarrierID: "car40907", Routing: domain.RoutingOpticalOnly},
	"guardian":                {CarrierID: "car308792", Routing: domain.RoutingOpticalOnly},
	"alivi":                   {CarrierID: "car308796", Routing: domain.RoutingOpticalOnly},
	"premier":                 {CarrierID: "car281317", Routing: domain.RoutingOpticalOnly},
	"envolve":                 {CarrierID: "car281245", Routing: domain.RoutingOpticalOnly},
	"sunhealth":               {CarrierID: "car308791", Routing: domain.RoutingOpticalOnly},
	"sunhealth discount plan": {CarrierID: "car308791", Routing: domain.RoutingOpticalOnly},
	"oscar":                   {CarrierID: "car284233", Routing: domain.RoutingOpticalOnly},
	"self pay":                {CarrierID: "car301672", Routing: domain.RoutingOpticalOnly},
}

var VisionInsuranceAliases = map[string]string{
	"eye med":                        "eyemed",
	"eye med vision":                 "eyemed",
	"eye med vision care":            "eyemed",
	"national vision":                "nva",
	"national vision administrators": "nva",
	"davis vision":                   "davis",
	"spectera vision":                "spectera",
	"soltice":                        "solstice",
	"solstice vision":                "solstice",
	"guardian vision":                "guardian",
	"alivi health":                   "alivi",
	"envolve vision":                 "envolve",
	"sun health":                     "sunhealth",
	"sunhealth vision":               "sunhealth",
	"oscar health":                   "oscar",
	"oscar insurance":                "oscar",
	"self-pay":                       "self pay",
	"selfpay":                        "self pay",
	"cash pay":                       "self pay",
	"cash":                           "self pay",

	"metlife":           "vsp",
	"liberty financial": "vsp",
	"lincoln financial": "vsp",
	"lincoln finacial":  "vsp",

	"humana": "eyemed",
	"aetna":  "eyemed",
	"unum":   "eyemed",
	"cigna":  "eyemed",

	"superior":     "davis",
	"florida blue": "davis",
	"blueview":     "davis",
	"blue view":    "davis",
	"versant":      "davis",

	"united healthcare":  "spectera",
	"united health care": "spectera",
	"united vision":      "spectera",

	"aetna better health":                            "icare",
	"aetna better health medicaid mma vision":        "icare",
	"aetna better health medicaid mma (vision)":      "icare",
	"aetna healthy kids kid care chip vision":        "icare",
	"aetna healthy kids kid care (chip) (vision)":    "icare",
	"aetna medicare hmo & ppo vision":                "icare",
	"aetna medicare hmo & ppo (vision)":              "icare",
	"aetna medicare ppo vision":                      "icare",
	"aetna medicare ppo (vision) effective 1 1 2026": "icare",
	"avmed":                          "icare",
	"avmed entrust vision":           "icare",
	"avmed entrust (vision)":         "icare",
	"community care plan vision":     "icare",
	"doctors health medicare vision": "icare",
	"doctors health medicare (vision) effective 8 1 2023": "icare",
	"eye care":                         "icare",
	"eye care health":                  "icare",
	"eye care solutions":               "icare",
	"freedom":                          "icare",
	"freedom health medicare vision":   "icare",
	"freedom health medicare (vision)": "icare",
	"healthsun vision":                 "icare",
	"healthsun vision only":            "icare",
	"humana (medicaid) vision":         "icare",
	"humana (medicare) vision":         "icare",
	"humana gold plus":                 "icare",
	"humana medicaid vision":           "icare",
	"humana medicare vision":           "icare",
	"i care":                           "icare",
	"miami children's health plan (medicaid) vision":      "icare",
	"miami children's health plan medicaid vision":        "icare",
	"molina medicaid vision":                              "icare",
	"molina medicaid (vision)":                            "icare",
	"optimum":                                             "icare",
	"optimum healthcare":                                  "icare",
	"optimum healthplan medicare vision":                  "icare",
	"optimum healthplan medicare (vision)":                "icare",
	"preferred care network - previously medica (vision)": "icare",
	"preferred care network previously medica vision":     "icare",
	"simply medcaid":                                      "icare",
	"simply medicaid":                                     "icare",
	"simply medicaid healthy kids (vision)":               "icare",
	"simply medicaid healthy kids vision":                 "icare",
	"simply medicare":                                     "icare",
	"simply medicare (vision)":                            "icare",
	"simply medicare vision":                              "icare",

	"ambetter":                             "envolve",
	"ambetter (vision)":                    "envolve",
	"ambetter vision":                      "envolve",
	"ambetter from sunshine health":        "envolve",
	"children's medical services (vision)": "envolve",
	"children's medical services vision":   "envolve",
	"staywell medicaid (vision)":           "envolve",
	"staywell medicaid vision":             "envolve",
	"sunshine":                             "envolve",
	"sunshine health":                      "envolve",
	"sunshine medicaid (vision)":           "envolve",
	"sunshine medicaid vision":             "envolve",
	"wellcare (medicaid) vision":           "envolve",
	"wellcare medicaid vision":             "envolve",

	"amerihealth":                              "premier",
	"devoted":                                  "premier",
	"devoted medicare hmo (vision)":            "premier",
	"devoted medicare hmo vision":              "premier",
	"devoted medicare ppo (vision)":            "premier",
	"devoted medicare ppo vision":              "premier",
	"florida blue medicare hmo & ppo (vision)": "premier",
	"florida blue medicare hmo & ppo vision":   "premier",
	"solis medicare (vision)":                  "premier",
	"solis medicare vision":                    "premier",
	"wellcare medicare hmo (vision)":           "premier",
	"wellcare medicare hmo vision":             "premier",
}

func lookupInsuranceEntry(name string, entries map[string]InsuranceEntry, aliases map[string]string) (InsuranceEntry, string, bool) {
	normalized := domain.NormalizeForLookup(name)

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
		return VisionInsuranceNameMap["icare"], true
	}
	return lookupInsuranceFromMaps(name, VisionInsuranceNameMap, VisionInsuranceAliases)
}

func isAetnaGovernmentVisionPlan(name string) bool {
	words := strings.FieldsFunc(domain.NormalizeForLookup(name), func(r rune) bool {
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

func CanonicalInsuranceName(name string) (string, bool) {
	normalized := domain.NormalizeForLookup(name)
	for _, plan := range medicalPlans {
		if domain.NormalizeForLookup(plan.Name) == normalized {
			return normalized, true
		}
	}
	canonical := ""
	for _, plan := range medicalPlans {
		for _, alias := range plan.Aliases {
			if domain.NormalizeForLookup(alias) == normalized {
				key := domain.NormalizeForLookup(plan.Name)
				if canonical != "" && canonical != key {
					return "", false
				}
				canonical = key
			}
		}
	}
	if canonical != "" {
		return canonical, true
	}
	if _, ok := VisionInsuranceNameMap[normalized]; ok {
		return normalized, true
	}
	switch normalized {
	case "eye med", "eye med vision", "eye med vision care", "national vision", "national vision administrators",
		"davis vision", "spectera vision", "soltice", "solstice vision", "guardian vision", "alivi health", "sunhealth vision", "i care":
		return VisionInsuranceAliases[normalized], true
	}
	return "", false
}

func IsSelfPayInsurance(name string) bool {
	normalized := domain.NormalizeForLookup(name)
	return normalized == "self pay" ||
		normalized == "selfpay" || normalized == "cash" || normalized == "cash pay" ||
		VisionInsuranceAliases[normalized] == "self pay"
}
