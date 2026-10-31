package insurance

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"advancedmd-token-management/internal/domain"
)

const (
	ruleDuplicateCarrier       CatalogRule = "duplicate_carrier"
	ruleDuplicateRegion        CatalogRule = "duplicate_region"
	ruleDuplicateTable         CatalogRule = "duplicate_table"
	ruleTableWithoutOffices    CatalogRule = "table_without_offices"
	ruleUnknownOffice          CatalogRule = "unknown_office"
	ruleOfficeInTwoTables      CatalogRule = "office_in_two_tables"
	ruleOfficeWithoutTable     CatalogRule = "office_without_table"
	ruleUnknownDoctorColumn    CatalogRule = "unknown_doctor_column"
	ruleMissingDoctorColumn    CatalogRule = "missing_doctor_column"
	ruleDuplicateDoctorColumn  CatalogRule = "duplicate_doctor_column"
	ruleInvalidPlanID          CatalogRule = "invalid_plan_id"
	ruleDuplicatePlan          CatalogRule = "duplicate_plan"
	rulePlanIDReused           CatalogRule = "plan_id_reused"
	ruleInvalidFamily          CatalogRule = "invalid_family"
	ruleUnknownCoverage        CatalogRule = "unknown_coverage"
	ruleFillerOnlyName         CatalogRule = "filler_only_name"
	ruleRepeatedName           CatalogRule = "repeated_name"
	ruleNameConflict           CatalogRule = "name_conflict"
	ruleUnknownCarrier         CatalogRule = "unknown_carrier"
	ruleCarrierCodeConflict    CatalogRule = "carrier_code_conflict"
	rulePlanInNoTable          CatalogRule = "plan_in_no_table"
	ruleUnknownPlan            CatalogRule = "unknown_plan"
	ruleDuplicateRow           CatalogRule = "duplicate_row"
	ruleInvalidDoctorValue     CatalogRule = "invalid_doctor_value"
	ruleAcceptedWithoutCarrier CatalogRule = "accepted_plan_without_carrier"
	ruleOnlyOfficeOutsideTable CatalogRule = "only_office_outside_table"
	ruleUnknownRequirement     CatalogRule = "unknown_requirement"
	ruleTwoSelfPayPlans        CatalogRule = "two_self_pay_plans"
	ruleNoteRuleNotEncoded     CatalogRule = "note_rule_not_encoded"
	ruleNameDoesNotResolve     CatalogRule = "name_does_not_resolve"
	ruleNoAcceptedSelfPay      CatalogRule = "no_accepted_self_pay"
	ruleContradictedPhrasing   CatalogRule = "contradicted_phrasing"
)

var planIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type catalogCheck struct {
	errs              []CatalogError
	doctorColumnDrift []CatalogError
}

func (c *catalogCheck) fail(region, table, planID string, rule CatalogRule, format string, args ...any) {
	c.errs = append(c.errs, CatalogError{Region: region, Table: table, PlanID: planID, Rule: rule, Message: fmt.Sprintf(format, args...)})
}

func (c *catalogCheck) drift(region, table string, rule CatalogRule, format string, args ...any) {
	c.doctorColumnDrift = append(c.doctorColumnDrift, CatalogError{Region: region, Table: table, Rule: rule, Message: fmt.Sprintf(format, args...)})
}

func parseCatalog(source InsuranceCatalog) (lists []planList, errs, drift []CatalogError) {
	lists, errs, drift = readPlanLists(source)
	if len(errs) > 0 {
		return lists, errs, drift
	}
	c := &catalogCheck{}
	c.decisions(lists)
	return lists, c.errs, drift
}

func readPlanLists(source InsuranceCatalog) ([]planList, []CatalogError, []CatalogError) {
	c := &catalogCheck{}
	carriers := c.carriers(source.Carriers)
	regions := c.distinctRegions(source.Regions)
	c.officeTables(regions)
	lists := []planList{}
	planByID := map[string]plan{}
	carrierForCode := map[string]string{}
	for _, region := range regions {
		plans := c.regionPlans(region, carriers, planByID, carrierForCode)
		used := map[string]bool{}
		for _, table := range region.Tables {
			list := c.table(region.ID, table, plans, used)
			indexNames(&list)
			c.tableNames(list)
			lists = append(lists, list)
		}
		for _, p := range region.Plans {
			if _, ok := plans[p.ID]; ok && !used[p.ID] {
				c.fail(region.ID, "", p.ID, rulePlanInNoTable, "plan is in no office table")
				used[p.ID] = true
			}
		}
	}
	c.notes(lists)
	return lists, c.errs, c.doctorColumnDrift
}

func (c *catalogCheck) carriers(carriers []CatalogCarrier) map[string]string {
	names := map[string]string{}
	for _, carrier := range carriers {
		if _, ok := names[carrier.ID]; ok {
			c.fail("", "", "", ruleDuplicateCarrier, "carrier %s is listed twice", carrier.ID)
			continue
		}
		names[carrier.ID] = carrier.Name
	}
	return names
}

func (c *catalogCheck) distinctRegions(regions []CatalogRegion) []CatalogRegion {
	kept := []CatalogRegion{}
	seen := map[string]bool{}
	for _, region := range regions {
		if seen[region.ID] {
			c.fail(region.ID, "", "", ruleDuplicateRegion, "region %q is listed twice", region.ID)
			continue
		}
		seen[region.ID] = true
		tables := []CatalogTable{}
		seenTables := map[string]bool{}
		for _, table := range region.Tables {
			if seenTables[table.ID] {
				c.fail(region.ID, table.ID, "", ruleDuplicateTable, "table %q is listed twice", table.ID)
				continue
			}
			seenTables[table.ID] = true
			tables = append(tables, table)
		}
		region.Tables = tables
		kept = append(kept, region)
	}
	return kept
}

func (c *catalogCheck) officeTables(regions []CatalogRegion) {
	tableForOffice := map[string]string{}
	for _, region := range regions {
		for _, table := range region.Tables {
			if len(table.Offices) == 0 {
				c.fail(region.ID, table.ID, "", ruleTableWithoutOffices, "table has no offices")
			}
			for _, office := range table.Offices {
				if _, ok := domain.TableOfficeProviderNames(office); !ok {
					c.fail(region.ID, table.ID, "", ruleUnknownOffice, "unknown office %q", office)
					continue
				}
				if other, ok := tableForOffice[office]; ok {
					c.fail(region.ID, table.ID, "", ruleOfficeInTwoTables, "office %q is also in %s", office, other)
					continue
				}
				tableForOffice[office] = region.ID + "/" + table.ID
			}
		}
	}
	for _, office := range domain.TableOfficeIDs() {
		if _, ok := tableForOffice[office]; !ok {
			c.fail("", "", "", ruleOfficeWithoutTable, "office %q has no table", office)
		}
	}
}

func (c *catalogCheck) regionPlans(region CatalogRegion, carriers map[string]string, planByID map[string]plan, carrierForCode map[string]string) map[string]plan {
	plans := map[string]plan{}
	for _, source := range region.Plans {
		p := plan{ID: source.ID, Label: source.Label, Family: source.Family, Coverage: source.Coverage, Names: source.Aliases, CarrierCode: source.CarrierCode, CarrierID: source.CarrierID, CarrierName: carriers[source.CarrierID], SelfPay: source.SelfPay}
		if _, ok := plans[p.ID]; ok {
			c.fail(region.ID, "", p.ID, ruleDuplicatePlan, "plan id is listed twice")
			continue
		}
		plans[p.ID] = p
		c.plan(region.ID, p, carriers)
		if other, ok := planByID[p.ID]; ok && (other.CarrierID != p.CarrierID || other.Coverage != p.Coverage) {
			c.fail(region.ID, "", p.ID, rulePlanIDReused, "plan id is used by another region for a different plan")
		}
		planByID[p.ID] = p
		if p.CarrierCode == "" {
			continue
		}
		if id, ok := carrierForCode[p.CarrierCode]; ok && id != p.CarrierID {
			c.fail(region.ID, "", p.ID, ruleCarrierCodeConflict, "carrier code %s maps to %s and %s", p.CarrierCode, id, p.CarrierID)
			continue
		}
		carrierForCode[p.CarrierCode] = p.CarrierID
	}
	return plans
}

func (c *catalogCheck) plan(region string, p plan, carriers map[string]string) {
	fail := func(rule CatalogRule, format string, args ...any) { c.fail(region, "", p.ID, rule, format, args...) }
	if !planIDPattern.MatchString(p.ID) {
		fail(ruleInvalidPlanID, "id %q must be lowercase kebab case", p.ID)
	}
	if p.Family == "" || p.Family != strings.TrimSpace(p.Family) {
		fail(ruleInvalidFamily, "family %q is empty or has surrounding spaces", p.Family)
	}
	if p.Coverage != "medical" && p.Coverage != "routine_vision" {
		fail(ruleUnknownCoverage, "unknown coverage %q", p.Coverage)
	}
	repeated := map[string]bool{}
	for _, name := range planNames(p) {
		words := tokens(name)
		if len(words) == 0 {
			fail(ruleFillerOnlyName, "name %q has only filler words", name)
			continue
		}
		key := strings.Join(words, " ")
		if repeated[key] {
			fail(ruleRepeatedName, "name %q is repeated", name)
		}
		repeated[key] = true
	}
	if p.CarrierID != "" && carriers[p.CarrierID] == "" {
		fail(ruleUnknownCarrier, "carrierId %s is not in carriers", p.CarrierID)
	}
}

func (c *catalogCheck) table(region string, source CatalogTable, plans map[string]plan, used map[string]bool) planList {
	c.doctorColumns(region, source)
	list := planList{Region: region, Table: source.ID, Offices: source.Offices}
	seen := map[string]bool{}
	for _, row := range source.Rows {
		fail := func(rule CatalogRule, format string, args ...any) {
			c.fail(region, source.ID, row.PlanID, rule, format, args...)
		}
		p, ok := plans[row.PlanID]
		if !ok {
			fail(ruleUnknownPlan, "plan %q is not in the region's plans", row.PlanID)
			continue
		}
		if seen[p.ID] {
			fail(ruleDuplicateRow, "plan has two rows")
			continue
		}
		seen[p.ID] = true
		used[p.ID] = true
		anyYes := false
		for _, doctor := range slices.Sorted(maps.Keys(row.Doctors)) {
			if !slices.Contains(source.Doctors, doctor) {
				fail(ruleUnknownDoctorColumn, "doctor %q is not a column of this table", doctor)
			}
			switch value := row.Doctors[doctor]; value {
			case "yes":
				anyYes = true
			case "no", "pending":
			default:
				fail(ruleInvalidDoctorValue, "doctor %q has value %q, not yes, no, or pending", doctor, value)
			}
		}
		if anyYes && p.CarrierID == "" {
			fail(ruleAcceptedWithoutCarrier, "accepted plan has no carrierId")
		}
		for _, office := range row.OnlyOffices {
			if !slices.Contains(source.Offices, office) {
				fail(ruleOnlyOfficeOutsideTable, "onlyOffices has %q, which is not an office of this table", office)
			}
		}
		for _, r := range row.Requires {
			if r.Kind != "prior_authorization" && r.Kind != "pcp_referral" && r.Kind != "staff_verify" {
				fail(ruleUnknownRequirement, "unknown requirement %q", r.Kind)
			}
			p.Requirements = append(p.Requirements, planRequirement{Kind: r.Kind, Channel: r.Channel})
		}
		p.Doctors = row.Doctors
		p.OnlyOffices = row.OnlyOffices
		p.CallerNotice = row.Notice
		p.Note = row.Note
		list.Plans = append(list.Plans, p)
	}
	return list
}

func (c *catalogCheck) doctorColumns(region string, table CatalogTable) {
	if len(table.Offices) == 0 {
		return
	}
	want := []string{}
	for _, office := range table.Offices {
		names, ok := domain.TableOfficeProviderNames(office)
		if !ok {
			return
		}
		for _, name := range names {
			if !slices.Contains(want, name) {
				want = append(want, name)
			}
		}
	}
	slices.Sort(want)
	seen := map[string]bool{}
	for _, doctor := range table.Doctors {
		if seen[doctor] {
			c.fail(region, table.ID, "", ruleDuplicateDoctorColumn, "doctor column %q is listed twice", doctor)
			continue
		}
		seen[doctor] = true
		if !slices.Contains(want, doctor) {
			c.drift(region, table.ID, ruleUnknownDoctorColumn, "doctor column %q is not a doctor at %s", doctor, strings.Join(table.Offices, ", "))
		}
	}
	for _, doctor := range want {
		if !seen[doctor] {
			c.drift(region, table.ID, ruleMissingDoctorColumn, "doctor column %q is missing", doctor)
		}
	}
}

func (c *catalogCheck) tableNames(list planList) {
	owners := map[string]string{}
	selfPay := map[string]string{}
	for _, p := range list.Plans {
		if p.SelfPay {
			if other, ok := selfPay[p.Coverage]; ok {
				c.fail(list.Region, list.Table, p.ID, ruleTwoSelfPayPlans, "%s and %s are both self-pay plans for %s", other, p.ID, p.Coverage)
			} else {
				selfPay[p.Coverage] = p.ID
			}
		}
		for _, name := range p.nameTokens {
			if len(name) == 0 {
				continue
			}
			key := p.Coverage + ":" + strings.Join(name, " ")
			owner, ok := owners[key]
			if ok && owner != p.ID {
				c.fail(list.Region, list.Table, p.ID, ruleNameConflict, "name %q also belongs to %s", key, owner)
			}
			if !ok {
				owners[key] = p.ID
			}
		}
	}
}

func indexNames(list *planList) {
	list.vocabulary = map[string]map[string]bool{}
	for i := range list.Plans {
		p := &list.Plans[i]
		if list.vocabulary[p.Coverage] == nil {
			list.vocabulary[p.Coverage] = map[string]bool{}
		}
		for _, name := range planNames(*p) {
			words := tokens(name)
			p.nameTokens = append(p.nameTokens, words)
			for _, word := range words {
				list.vocabulary[p.Coverage][word] = true
				if planTypes[word] {
					if p.planTypes == nil {
						p.planTypes = map[string]bool{}
					}
					p.planTypes[word] = true
				}
			}
		}
	}
}
