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
	Requirements    []InsuranceRequirement         `json:"requirements"`
	Providers       []string                       `json:"credentialedProviders"`
	Issue           string                         `json:"issue"`
	CarrierIssue    string                         `json:"carrierIssue"`
	OfficeIssues    map[string]string              `json:"officeIssues"`
	Offices         map[string]medicalOfficePolicy `json:"offices"`
}

type medicalOfficePolicy struct {
	Status  string      `json:"status"`
	Routing RoutingRule `json:"routing"`
	Notice  string      `json:"notice"`
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
				OfficeUnverified: !configured, Status: policy.Status, Canonical: canonical, Display: p.Name, Aliases: p.Aliases,
				RequiredAliases: p.RequiredAliases, Notice: policy.Notice, Clarification: p.Clarification,
				CarrierID: p.CarrierID, CarrierCode: p.CarrierCode, Routing: policy.Routing,
				Requirements: p.Requirements, Providers: p.Providers, Issue: p.Issue,
				CarrierIssue: p.CarrierIssue, OfficeIssues: p.OfficeIssues,
			})
		}
	}
	return result
}
