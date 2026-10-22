package domain

import (
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"
)

type OfficeConfig struct {
	ID                         string
	DisplayName                string
	FacilityID                 string
	DefaultProfileID           string
	Columns                    map[string]OfficeColumn
	RoutingTiers               map[RoutingRule][]string
	PediatricRouting           RoutingRule
	AppointmentLookupOfficeIDs []string
	AppointmentTypes           OfficeAppointmentTypes
	RoutineVisionMinimumAge    MinimumAge
}

type MinimumAge struct {
	Years   int
	Message string
}

type OfficeColumn struct {
	ProfileID         string
	DisplayName       string
	ShortName         string
	MatchKey          string
	MinAgeYears       int
	SameStartCapacity int
	SameStartWindows  []SameStartWindow
}

type SameStartWindow struct {
	Weekday     time.Weekday
	StartMinute int
	EndMinute   int
}

func ResolveOffice(name string) (*OfficeConfig, error) {
	if name == "" {
		return DefaultOffice(), nil
	}
	office, ok := LookupOffice(name)
	if !ok {
		return nil, fmt.Errorf("unknown office: %q. Valid options: %s", name, strings.Join(validOfficeNames(), ", "))
	}
	return office, nil
}

func (c OfficeColumn) SameStartCapacityAt(start time.Time) int {
	if c.SameStartCapacity <= 0 {
		return 0
	}
	if len(c.SameStartWindows) == 0 {
		return c.SameStartCapacity
	}

	minute := start.Hour()*60 + start.Minute()
	for _, window := range c.SameStartWindows {
		if window.Weekday == start.Weekday() && minute >= window.StartMinute && minute <= window.EndMinute {
			return c.SameStartCapacity
		}
	}
	return 0
}

func sameStartWindow(weekday time.Weekday, startHour, startMinute, endHour, endMinute int) SameStartWindow {
	return SameStartWindow{
		Weekday:     weekday,
		StartMinute: startHour*60 + startMinute,
		EndMinute:   endHour*60 + endMinute,
	}
}

func (o *OfficeConfig) AllowedColumnIDs() []string {
	ids := make([]string, 0, len(o.Columns))
	for id := range o.Columns {
		ids = append(ids, id)
	}
	return ids
}

func (o *OfficeConfig) ColumnsForRouting(rule RoutingRule) map[string]bool {
	colIDs, ok := o.columnIDsForRouting(rule)
	if !ok {
		return nil
	}

	result := make(map[string]bool, len(colIDs))
	for _, id := range colIDs {
		result[id] = true
	}
	return result
}

func (o *OfficeConfig) ColumnsForRoutingAndDOB(rule RoutingRule, dob string, asOf time.Time) map[string]bool {
	cols := o.ColumnsForRouting(rule)
	if cols == nil {
		return cols
	}

	filtered := make(map[string]bool, len(cols))
	for id := range cols {
		if dob == "" || o.ColumnAllowsDOB(id, dob, asOf) {
			filtered[id] = true
		}
	}
	return filtered
}

func (o *OfficeConfig) ProvidersForRouting(rule RoutingRule) []string {
	colIDs, ok := o.columnIDsForRouting(rule)
	if !ok {
		return nil
	}
	return o.providerNamesForColumnIDs(colIDs, nil)
}

func (o *OfficeConfig) ProvidersForRoutingAndDOB(rule RoutingRule, dob string, asOf time.Time) []string {
	colIDs, ok := o.columnIDsForRouting(rule)
	if !ok {
		return nil
	}
	colMap := o.ColumnsForRoutingAndDOB(rule, dob, asOf)
	return o.providerNamesForColumnIDs(colIDs, colMap)
}

func (o *OfficeConfig) ColumnAllowsDOB(columnID, dob string, asOf time.Time) bool {
	col, ok := o.Columns[columnID]
	if !ok {
		return false
	}
	if col.MinAgeYears == 0 {
		return true
	}
	age, ok := AgeYearsOn(dob, asOf)
	if !ok {
		return false
	}
	return age >= col.MinAgeYears
}

func (o *OfficeConfig) columnIDsForRouting(rule RoutingRule) ([]string, bool) {
	if rule == RoutingNotAccepted {
		return nil, false
	}

	colIDs, ok := o.RoutingTiers[rule]
	if ok {
		return colIDs, true
	}
	if rule == RoutingOpticalOnly {
		return []string{}, true
	}
	return o.RoutingTiers[RoutingAll], true
}

func (o *OfficeConfig) providerNamesForColumnIDs(colIDs []string, allowed map[string]bool) []string {
	names := make([]string, 0, len(colIDs))
	seen := make(map[string]bool)
	for _, id := range colIDs {
		if allowed != nil && !allowed[id] {
			continue
		}
		col, ok := o.Columns[id]
		if !ok || seen[col.ShortName] {
			continue
		}
		seen[col.ShortName] = true
		names = append(names, col.ShortName)
	}
	return names
}

func (o *OfficeConfig) FriendlyProviderName(amdName string) string {
	upper := strings.ToUpper(amdName)
	match := ""
	for _, col := range o.Columns {
		if col.MatchKey != "" && strings.Contains(upper, col.MatchKey) {
			if match == "" || len(col.DisplayName) < len(match) {
				match = col.DisplayName
			}
		}
	}
	if match != "" {
		return match
	}
	return amdName
}

func (o *OfficeConfig) AppointmentColor(typeID int) (string, bool) {
	appointmentType, ok := AppointmentTypes[typeID]
	return appointmentType.Color, ok
}

func (o *OfficeConfig) AppointmentTypeName(typeID int) (string, bool) {
	appointmentType, ok := AppointmentTypes[typeID]
	return appointmentType.Name, ok
}

func TableOfficeIDs() []string {
	ids := make([]string, 0, len(officeData.Offices))
	for _, definition := range officeData.Offices {
		ids = append(ids, definition.ID)
	}
	sort.Strings(ids)
	return ids
}

func TableOfficeProviderNames(officeID string) ([]string, bool) {
	for _, definition := range officeData.Offices {
		if definition.ID != officeID {
			continue
		}
		names := []string{}
		for _, column := range definition.Columns {
			if !slices.Contains(names, column.Provider.DisplayName) {
				names = append(names, column.Provider.DisplayName)
			}
		}
		sort.Strings(names)
		return names, true
	}
	return nil, false
}

func LookupOfficeByID(officeID string) (*OfficeConfig, bool) {
	for _, office := range selected().offices {
		if office.ID == officeID {
			return office, true
		}
	}
	return nil, false
}

const PreservedAppointmentTypeFallbackColor = "TEAL"

func ResolveAppointmentTypeID(typeID int) (int, bool) {
	providerTypeID, ok := selected().providerTypeIDs[typeID]
	return providerTypeID, ok
}

func CanonicalAppointmentTypeID(typeID int) (int, bool) {
	canonicalID, ok := selected().canonicalTypeIDs[typeID]
	return canonicalID, ok
}

func (o *OfficeConfig) AllowsAppointmentType(typeID int, routing RoutingRule) bool {
	if _, ok := AppointmentTypes[typeID]; !ok {
		return false
	}
	if routing == RoutingOpticalOnly {
		return len(o.RoutingTiers[RoutingOpticalOnly]) > 0 && o.AppointmentTypes.RoutineVision.includes(typeID)
	}
	if len(o.ColumnsForRouting(routing)) == 0 {
		return false
	}
	return typeID == o.AppointmentTypes.PostOp || o.AppointmentTypes.Medical.includes(typeID)
}

type registry struct {
	offices          []*OfficeConfig
	byPhone          map[string]*OfficeConfig
	defaultOffice    *OfficeConfig
	providerTypeIDs  map[int]int
	canonicalTypeIDs map[int]int
}

var officeData = newOfficeTable()

var registries = mustBuildRegistries(officeData)

func mustBuildRegistries(table officeTable) map[Environment]*registry {
	if err := validateOfficeTable(table); err != nil {
		panic("invalid office table: " + err.Error())
	}
	return buildRegistries(table)
}

var current *registry

func selected() *registry {
	if current == nil {
		panic("domain: InitRegistry must select an environment before office lookups")
	}
	return current
}

func buildRegistries(table officeTable) map[Environment]*registry {
	built := make(map[Environment]*registry, len(table.Environments))
	for env, binding := range table.Environments {
		built[env] = buildRegistry(table.Offices, binding)
	}
	return built
}

func buildRegistry(definitions []officeDefinition, binding environmentBinding) *registry {
	r := &registry{
		byPhone:          map[string]*OfficeConfig{},
		providerTypeIDs:  make(map[int]int, len(binding.AppointmentTypes)),
		canonicalTypeIDs: make(map[int]int, len(binding.AppointmentTypes)),
	}
	byID := map[string]*OfficeConfig{}
	for _, definition := range definitions {
		officeBinding, ok := binding.Offices[definition.ID]
		if !ok {
			continue
		}
		office := bindOffice(definition, officeBinding)
		r.offices = append(r.offices, office)
		byID[office.ID] = office
		for _, phone := range officeBinding.Phones {
			r.byPhone[phone] = office
		}
	}
	for _, definition := range definitions {
		office, ok := byID[definition.ID]
		if !ok {
			continue
		}
		for _, id := range definition.AppointmentLookupOfficeIDs {
			if _, available := byID[id]; available {
				office.AppointmentLookupOfficeIDs = append(office.AppointmentLookupOfficeIDs, id)
			}
		}
	}
	r.defaultOffice = byID[defaultOfficeID]
	for canonicalID, providerID := range binding.AppointmentTypes {
		r.providerTypeIDs[canonicalID] = providerID
		r.canonicalTypeIDs[providerID] = canonicalID
	}
	return r
}

func bindOffice(definition officeDefinition, binding officeBinding) *OfficeConfig {
	office := &OfficeConfig{
		ID:                      definition.ID,
		DisplayName:             definition.DisplayName,
		FacilityID:              binding.FacilityID,
		DefaultProfileID:        binding.DefaultProfileID,
		Columns:                 make(map[string]OfficeColumn, len(binding.Columns)),
		RoutingTiers:            make(map[RoutingRule][]string, len(definition.RoutingTiers)),
		PediatricRouting:        definition.PediatricRouting,
		AppointmentTypes:        definition.AppointmentTypes,
		RoutineVisionMinimumAge: definition.RoutineVisionMinimumAge,
	}
	for key, column := range definition.Columns {
		columnBinding, ok := binding.Columns[key]
		if !ok {
			continue
		}
		office.Columns[columnBinding.ColumnID] = OfficeColumn{
			ProfileID:         columnBinding.ProfileID,
			DisplayName:       column.Provider.DisplayName,
			ShortName:         column.Provider.ShortName,
			MatchKey:          column.Provider.MatchKey,
			MinAgeYears:       column.MinAgeYears,
			SameStartCapacity: column.SameStartCapacity,
			SameStartWindows:  column.SameStartWindows,
		}
	}
	for rule, keys := range definition.RoutingTiers {
		columnIDs := make([]string, 0, len(keys))
		for _, key := range keys {
			if columnBinding, ok := binding.Columns[key]; ok {
				columnIDs = append(columnIDs, columnBinding.ColumnID)
			}
		}
		office.RoutingTiers[rule] = columnIDs
	}
	return office
}

type Environment string

const (
	Prod    Environment = "prod"
	Sandbox Environment = "sandbox"
)

var environments = []Environment{Prod, Sandbox}

func ParseEnvironment(value string) (Environment, error) {
	env := Environment(value)
	if !slices.Contains(environments, env) {
		return "", fmt.Errorf("environment must be one of %v", environments)
	}
	return env, nil
}

func InitRegistry(env Environment) {
	r, ok := registries[env]
	if !ok {
		panic(fmt.Sprintf("domain: no office registry for environment %q", env))
	}
	current = r
	log.Printf("Office registry: %s", env)
}

func stripToDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func NormalizePhoneDigits(s string) string {
	digits := stripToDigits(s)
	if len(digits) == 11 && digits[0] == '1' {
		return digits[1:]
	}
	return digits
}

func LookupOffice(phone string) (*OfficeConfig, bool) {
	phone = strings.TrimSpace(phone)
	for _, key := range officePhoneLookupKeys(phone) {
		if office, ok := selected().byPhone[key]; ok {
			return office, true
		}
	}

	lookup := normalizeOfficeLookup(phone)
	compactLookup := strings.ReplaceAll(lookup, " ", "")
	for _, office := range selected().offices {
		for _, candidate := range []string{office.ID, office.DisplayName} {
			normalized := normalizeOfficeLookup(candidate)
			if lookup == normalized || compactLookup == strings.ReplaceAll(normalized, " ", "") {
				return office, true
			}
		}
	}
	return nil, false
}

func officePhoneLookupKeys(phone string) []string {
	if phone == "" {
		return nil
	}

	keys := []string{phone}
	if phone[0] != '+' {
		keys = append(keys, "+"+phone)
	}

	digits := stripToDigits(phone)
	switch {
	case len(digits) == 10:
		keys = append(keys, "+1"+digits)
	case len(digits) == 11 && digits[0] == '1':
		keys = append(keys, "+"+digits)
	}

	return keys
}

func DefaultOffice() *OfficeConfig {
	return selected().defaultOffice
}

func validOfficeNames() []string {
	names := make([]string, 0, len(selected().offices))
	for _, office := range selected().offices {
		names = append(names, office.DisplayName)
	}
	sort.Strings(names)
	return names
}

func normalizeOfficeLookup(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	return strings.Join(strings.Fields(s), " ")
}

func (o *OfficeConfig) SchedulingRouting(routing RoutingRule, dob string, asOf time.Time) RoutingRule {
	if routing == RoutingNotAccepted || routing == RoutingOpticalOnly || !isMinor(dob, asOf) {
		return routing
	}
	return o.PediatricRouting
}

func (o *OfficeConfig) SupportsRouting(routing RoutingRule) bool {
	return len(o.ColumnsForRouting(routing)) > 0
}

func (o *OfficeConfig) SupportsMedical() bool {
	return o.SupportsRouting(RoutingAll) || o.SupportsRouting(RoutingBachOnly) || o.SupportsRouting(RoutingBachLicht)
}
