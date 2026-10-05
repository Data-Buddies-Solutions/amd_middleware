package insurance

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"advancedmd-token-management/internal/domain"
)

var planIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validateOfficeTables(officeTables map[string][]string) error {
	tables := slices.Sorted(maps.Keys(officeTables))
	officeTable := map[string]string{}
	for _, table := range tables {
		if len(officeTables[table]) == 0 {
			return fmt.Errorf("%s: no offices", table)
		}
		for _, office := range officeTables[table] {
			if _, ok := domain.LookupOfficeByID(office); !ok {
				return fmt.Errorf("%s: unknown office %q", table, office)
			}
			if other, ok := officeTable[office]; ok {
				return fmt.Errorf("office %q is in %s and %s", office, other, table)
			}
			officeTable[office] = table
		}
	}
	for _, office := range domain.OfficeIDs() {
		if _, ok := officeTable[office]; !ok {
			return fmt.Errorf("office %q has no plan list", office)
		}
	}
	return nil
}

func validateCatalog(lists []planList, carriers map[string]string) error {
	carrierForCode := map[string]string{}
	planByID := map[string]plan{}
	for _, list := range lists {
		file := list.Table
		owners := map[string]string{}
		for _, p := range list.Plans {
			if err := validatePlan(p, list, carriers); err != nil {
				return fmt.Errorf("%s: plan %q: %w", file, p.ID, err)
			}
			if other, ok := planByID[p.ID]; ok && (other.CarrierID != p.CarrierID || other.Coverage != p.Coverage) {
				return fmt.Errorf("%s: plan id %q reused for a different plan", file, p.ID)
			}
			planByID[p.ID] = p
			if p.CarrierCode != "" {
				if id, ok := carrierForCode[p.CarrierCode]; ok && id != p.CarrierID {
					return fmt.Errorf("%s: carrier code %s maps to %s and %s", file, p.CarrierCode, id, p.CarrierID)
				}
				carrierForCode[p.CarrierCode] = p.CarrierID
			}
			for _, name := range p.nameTokens {
				key := p.Coverage + ":" + strings.Join(name, " ")
				if owner, ok := owners[key]; ok && owner != p.ID {
					return fmt.Errorf("%s: name %q belongs to %s and %s", file, key, owner, p.ID)
				}
				owners[key] = p.ID
			}
		}
	}
	return nil
}

func validatePlan(p plan, list planList, carriers map[string]string) error {
	if !planIDPattern.MatchString(p.ID) {
		return fmt.Errorf("id must be lowercase kebab case")
	}
	if p.Coverage != "medical" && p.Coverage != "routine_vision" {
		return fmt.Errorf("unknown coverage %q", p.Coverage)
	}
	repeated := map[string]bool{}
	for i, name := range p.nameTokens {
		if len(name) == 0 {
			return fmt.Errorf("name %q has only filler words", planNames(p)[i])
		}
		key := strings.Join(name, " ")
		if repeated[key] {
			return fmt.Errorf("name %q is repeated", planNames(p)[i])
		}
		repeated[key] = true
	}
	anyYes := false
	for doctor, value := range p.Doctors {
		switch value {
		case "yes":
			anyYes = true
		case "no", "pending":
		default:
			return fmt.Errorf("doctor %q has value %q", doctor, value)
		}
	}
	if anyYes && p.CarrierID == "" {
		return fmt.Errorf("accepted plan has no carrierId")
	}
	if p.CarrierID != "" && carriers[p.CarrierID] == "" {
		return fmt.Errorf("carrierId %s is not in %s", p.CarrierID, carriersFile)
	}
	for _, office := range p.OnlyOffices {
		if !slices.Contains(list.Offices, office) {
			return fmt.Errorf("onlyOffices has %q outside the list", office)
		}
	}
	for _, r := range p.Requirements {
		if r.Kind != "prior_authorization" && r.Kind != "pcp_referral" && r.Kind != "staff_verify" {
			return fmt.Errorf("unknown requirement %q", r.Kind)
		}
	}
	return nil
}
