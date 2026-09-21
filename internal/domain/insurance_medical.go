package domain

import "encoding/json"

// A medical plan is defined once. Offices contain only participation differences.
// Every accepted office policy has a complete carrier mapping.
type medicalPlan struct {
	Name            string                         `json:"name"`
	Aliases         []string                       `json:"aliases"`
	RequiredAliases []string                       `json:"requiredWordAliases"`
	CarrierCode     string                         `json:"carrierCode"`
	CarrierID       string                         `json:"carrierId"`
	Clarification   string                         `json:"clarification"`
	Providers       []string                       `json:"credentialedProviders"`
	Offices         map[string]medicalOfficePolicy `json:"offices"`
}

type medicalOfficePolicy struct {
	Status       string                 `json:"status"`
	Routing      RoutingRule            `json:"routing"`
	Notice       string                 `json:"notice"`
	Requirements []InsuranceRequirement `json:"requirements,omitempty"`
}

type medicalCatalogData struct {
	Plans        []medicalPlan     `json:"plans"`
	CarrierNames map[string]string `json:"carrierNames"`
}

var medicalCatalog = func() medicalCatalogData {
	b, err := insuranceSources.ReadFile("insurance_data/MEDICAL.json")
	if err != nil {
		panic(err)
	}
	var doc medicalCatalogData
	if err = json.Unmarshal(b, &doc); err != nil {
		panic(err)
	}
	seen := map[string]bool{}
	for _, p := range doc.Plans {
		key := insuranceNormalize(p.Name)
		if key == "" || seen[key] {
			panic("duplicate or empty medical plan name")
		}
		seen[key] = true
		for _, policy := range p.Offices {
			if policy.Status == "accepted" && (p.CarrierID == "" || policy.Routing == "" || policy.Routing == RoutingNotAccepted) {
				panic("accepted insurance requires carrier ID and office routing: " + p.Name)
			}
			switch policy.Status {
			case "accepted", "not_accepted", "needs_clarification", "needs_staff_task":
			default:
				panic("invalid medical participation status")
			}
		}
	}
	return doc
}()

var medicalPlans = medicalCatalog.Plans

func medicalRules() map[string][]participationRule {
	result := map[string][]participationRule{}
	for _, p := range medicalPlans {
		for _, office := range []string{"HOLLYWOOD_SWEETWATER", "SPRING_HILL_MEDICAL", "CRYSTAL_RIVER"} {
			policy, configured := p.Offices[office]
			if !configured {
				policy.Status = "needs_staff_task"
			}
			canonical := p.Name
			if policy.Status == "needs_clarification" {
				canonical = ""
			}
			result[office] = append(result[office], participationRule{
				Status: policy.Status, Canonical: canonical, Display: p.Name, Aliases: p.Aliases,
				RequiredAliases: p.RequiredAliases, Notice: policy.Notice, Clarification: p.Clarification,
				CarrierID: p.CarrierID, CarrierCode: p.CarrierCode, Routing: policy.Routing,
				Requirements: policy.Requirements, Providers: p.Providers,
			})
		}
	}
	return result
}

// A carrier directory label is not a medical product. Preserve an explicit
// chart product; otherwise use the product confirmed for this patient. The
// caller still verifies its carrier ID and applies current office requirements.
func medicalChartProduct(chart PatientDemographics, confirmed string) string {
	label := insuranceNormalize(chart.CarrierName)
	directory := insuranceNormalize(medicalCatalog.CarrierNames[chart.CarrierID])
	if label != "" && label != directory {
		// Resolve family aliases with the same matcher as caller input. A family
		// that asks for clarification must not discard the caller's answer.
		family := participationMatch("SPRING_HILL_MEDICAL", label)
		if family != nil && family.Clarification != "" && family.CarrierID == chart.CarrierID && confirmed != "" {
			return confirmed
		}
		for _, product := range medicalPlans {
			for _, name := range append([]string{product.Name}, product.Aliases...) {
				// A generic directory family is not an explicit product.
				term := insuranceNormalize(name)
				if term != directory && insuranceContains(label, term) {
					// Let the ordinary matcher preserve product specificity and
					// ambiguity, including labels decorated by the provider.
					return chart.CarrierName
				}
			}
			for _, name := range product.RequiredAliases {
				term := insuranceNormalize(name)
				if term != directory && insuranceContainsWords(label, term) {
					return chart.CarrierName
				}
			}
		}
	}
	if confirmed != "" {
		return confirmed
	}
	// A unique carrier/product mapping needs no caller clarification.
	productName := ""
	for _, product := range medicalPlans {
		if chart.CarrierID != "" && product.CarrierID == chart.CarrierID {
			if productName != "" {
				return ""
			}
			productName = product.Name
		}
	}
	return productName
}
