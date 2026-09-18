package domain

import "encoding/json"

// A medical plan is defined once. Offices contain only participation differences.
// An empty carrier ID means the billing identity still needs verification.
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
