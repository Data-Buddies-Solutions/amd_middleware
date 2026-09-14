package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// OfficeConfig defines the configuration for a single office location.
type OfficeConfig struct {
	dev              bool
	ID               string                   // "spring_hill"
	DisplayName      string                   // "Spring Hill"
	FacilityID       string                   // "1568"
	DefaultProfileID string                   // "620" (for addpatient XMLRPC)
	Columns          map[string]OfficeColumn  // column ID → config
	RoutingTiers     map[RoutingRule][]string // routing rule → column IDs
	PediatricRouting RoutingRule              // routing override for under-18
}

// OfficeColumn defines a provider column within an office.
type OfficeColumn struct {
	ProfileID         string            // "620"
	DisplayName       string            // "Dr. Austin Bach"
	ShortName         string            // "Dr. Bach"
	MatchKey          string            // "BACH" — uppercase fragment for matching AMD names
	MinAgeYears       int               // Minimum patient age in years; 0 means newborn and up
	SameStartCapacity int               // Maximum appointments at the same start time; 0 means single-booked
	SameStartWindows  []SameStartWindow // Optional allowed start windows; empty means all start times
}

// SameStartWindow limits second-bookable starts to an inclusive minute range on one weekday.
type SameStartWindow struct {
	Weekday     time.Weekday
	StartMinute int
	EndMinute   int
}

// ResolveOffice returns the requested office or the backward-compatible
// default when the caller omits it.
func (c *OfficeCatalog) ResolveOffice(name string) (*OfficeConfig, error) {
	if name == "" {
		return c.DefaultOffice(), nil
	}
	office, ok := c.LookupOffice(name)
	if !ok {
		return nil, fmt.Errorf("unknown office: %q. Valid options: %s", name, strings.Join(c.ValidOfficeNames(), ", "))
	}
	return office, nil
}

// SameStartCapacityAt returns the column's configured same-start capacity at a slot start time.
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

// InsuranceMode selects which insurance crosswalk should be used.
type InsuranceMode string

const (
	InsuranceModeMedical InsuranceMode = "medical"
	InsuranceModeVision  InsuranceMode = "vision"
)

// IsAllowedColumn checks if a column ID belongs to this office.
func (o *OfficeConfig) IsAllowedColumn(columnID string) bool {
	_, ok := o.Columns[columnID]
	return ok
}

// AllowedColumnIDs returns all column IDs for this office.
func (o *OfficeConfig) AllowedColumnIDs() []string {
	ids := make([]string, 0, len(o.Columns))
	for id := range o.Columns {
		ids = append(ids, id)
	}
	return ids
}

// ColumnsForRouting returns the allowed column IDs for a routing rule at this office.
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

// ColumnsForRoutingAndDOB returns routing columns filtered by provider age rules when DOB is known.
func (o *OfficeConfig) ColumnsForRoutingAndDOB(rule RoutingRule, dob string) map[string]bool {
	cols := o.ColumnsForRouting(rule)
	if cols == nil {
		return cols
	}

	filtered := make(map[string]bool, len(cols))
	for id := range cols {
		if o.ColumnAllowsDOB(id, dob) {
			filtered[id] = true
		}
	}
	return filtered
}

// ProvidersForRouting returns the display names for a routing rule at this office.
func (o *OfficeConfig) ProvidersForRouting(rule RoutingRule) []string {
	colIDs, ok := o.columnIDsForRouting(rule)
	if !ok {
		return nil
	}

	return o.providerNamesForColumnIDs(colIDs, nil)
}

// ProvidersForRoutingAndDOB returns allowed provider names after age filtering.
func (o *OfficeConfig) ProvidersForRoutingAndDOB(rule RoutingRule, dob string) []string {
	colIDs, ok := o.columnIDsForRouting(rule)
	if !ok {
		return nil
	}
	colMap := o.ColumnsForRoutingAndDOB(rule, dob)
	return o.providerNamesForColumnIDs(colIDs, colMap)
}

// ValidProviderNames returns all provider short names for this office.
func (o *OfficeConfig) ValidProviderNames() []string {
	names := make([]string, 0, len(o.Columns))
	seen := make(map[string]bool)
	for _, col := range o.Columns {
		if seen[col.ShortName] {
			continue
		}
		seen[col.ShortName] = true
		names = append(names, col.ShortName)
	}
	return names
}

// ColumnAllowsDOB reports whether a provider column can see a patient with the supplied DOB.
func (o *OfficeConfig) ColumnAllowsDOB(columnID, dob string) bool {
	col, ok := o.Columns[columnID]
	if !ok {
		return false
	}
	if col.MinAgeYears == 0 {
		return true
	}
	age, ok := AgeYears(dob)
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

// ProviderDisplayName returns the display name for a profile ID.
func (o *OfficeConfig) ProviderDisplayName(profileID string) string {
	for _, col := range o.Columns {
		if col.ProfileID == profileID {
			return col.DisplayName
		}
	}
	return ""
}

// FriendlyProviderName maps an AMD provider name to a friendly display name.
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

// AppointmentColor returns the booking color for an appointment type ID.
func (o *OfficeConfig) AppointmentColor(typeID int) (string, bool) {
	color, ok := appointmentTypeColors[typeID]
	return color, ok
}

// AppointmentTypeName returns the friendly name for an appointment type ID.
func (o *OfficeConfig) AppointmentTypeName(typeID int) (string, bool) {
	name, ok := appointmentTypeNames[typeID]
	return name, ok
}

// LookupOfficeByID resolves an office config from the active registry by stable
// office ID.
func (c *OfficeCatalog) LookupOfficeByID(officeID string) (*OfficeConfig, bool) {
	office, ok := c.offices[officeID]
	if !ok {
		return nil, false
	}
	return cloneOffice(office), true
}

// AppointmentLookupOfficeIDs returns the nearby-office IDs used when loading a
// resolved patient's upcoming appointments.
func (c *OfficeCatalog) AppointmentLookupOfficeIDs(office *OfficeConfig) []string {
	if office == nil {
		return nil
	}

	officeIDs := []string{office.ID}
	switch office.ID {
	case "spring_hill", "crystal_river":
		officeIDs = []string{"spring_hill", "crystal_river"}
	case "hollywood", "sweetwater":
		officeIDs = []string{"hollywood", "sweetwater"}
	}

	lookupIDs := make([]string, 0, len(officeIDs))
	seen := make(map[string]bool, len(officeIDs))
	for _, officeID := range officeIDs {
		lookupOffice, ok := c.LookupOfficeByID(officeID)
		if !ok {
			continue
		}
		if seen[lookupOffice.ID] {
			continue
		}
		seen[lookupOffice.ID] = true
		lookupIDs = append(lookupIDs, lookupOffice.ID)
	}
	if len(lookupIDs) == 0 {
		return []string{office.ID}
	}
	return lookupIDs
}

// PreservedAppointmentTypeFallbackColor is used when a signed existing type has
// no configured booking color.
const PreservedAppointmentTypeFallbackColor = "TEAL"

// appointmentTypeColors maps AMD appointment type IDs to booking colors.
var appointmentTypeColors = map[int]string{
	1006: "RED",    // New Adult Medical
	1004: "GREEN",  // New Pediatric Medical
	1007: "ORANGE", // Established Adult Medical (Follow Up)
	1005: "PINK",   // Established Pediatric Medical (Follow Up)
	1008: "BLUE",   // Post Op
	1010: "TEAL",   // New Adult Vision
	3364: "ROSE",   // Established Adult Vision
	4244: "BROWN",  // New Pediatric Vision
	4245: "GRAY",   // Established Pediatric Vision
	6167: "ORANGE", // Crystal River New Patient
	6168: "TEAL",   // Crystal River Post Op
	6169: "RED",    // Crystal River Established Patient
}

// appointmentTypeNames maps AMD appointment type IDs to friendly names.
var appointmentTypeNames = map[int]string{
	1006: "New Adult Medical",
	1004: "New Pediatric Medical",
	1007: "Established Adult Medical (Follow Up)",
	1005: "Established Pediatric Medical (Follow Up)",
	1008: "Post Op",
	1010: "New Adult Vision",
	3364: "Established Adult Vision",
	4244: "New Pediatric Vision",
	4245: "Established Pediatric Vision",
	6167: "Crystal River New Patient",
	6168: "Crystal River Post Op",
	6169: "Crystal River Established Patient",
}

var opticalAppointmentTypes = map[int]bool{
	1010: true,
	3364: true,
	4244: true,
	4245: true,
}

var crystalRiverAppointmentTypes = map[int]bool{
	6167: true,
	6168: true,
	6169: true,
}

// devAppointmentTypes maps prod type IDs to dev type IDs.
// Only used when AMD_ENV=dev; in prod the IDs pass through unchanged.
var devAppointmentTypes = map[int]int{
	1006: 12,   // New Adult Medical
	1004: 20,   // New Pediatric Medical
	1007: 18,   // Established Adult Medical (Follow Up)
	1005: 8,    // Established Pediatric Medical (Follow Up)
	1008: 1627, // Post Op
	// Vision and Crystal River have no configured sandbox mappings. Do not
	// substitute production IDs: resolution must fail until verified mappings exist.
}

// ResolveAppointmentTypeID translates a prod type ID to the env-specific ID.
// In prod, returns the ID unchanged. In dev, maps to the dev ID.
func (o *OfficeConfig) ResolveAppointmentTypeID(typeID int) (int, bool) {
	if _, ok := appointmentTypeColors[typeID]; !ok {
		return 0, false
	}
	if o.dev {
		devID, ok := devAppointmentTypes[typeID]
		return devID, ok
	}
	return typeID, true
}

// CanonicalAppointmentTypeID translates an env-specific AMD appointment type ID
// back to the canonical/prod ID accepted by booking requests.
func (o *OfficeConfig) CanonicalAppointmentTypeID(typeID int) (int, bool) {
	if !o.dev {
		if _, ok := appointmentTypeColors[typeID]; !ok {
			return 0, false
		}
		return typeID, true
	}

	for canonicalID, devID := range devAppointmentTypes {
		if devID == typeID {
			return canonicalID, true
		}
	}
	return 0, false
}

// AllowsAppointmentType reports whether an appointment type can be booked for this office/routing lane.
func (o *OfficeConfig) AllowsAppointmentType(typeID int, routing RoutingRule) bool {
	if _, ok := appointmentTypeColors[typeID]; !ok {
		return false
	}

	if routing == RoutingOpticalOnly {
		return len(o.RoutingTiers[RoutingOpticalOnly]) > 0 && opticalAppointmentTypes[typeID]
	}
	if len(o.ColumnsForRouting(routing)) == 0 {
		return false
	}
	if o.ID == "crystal_river" {
		return crystalRiverAppointmentTypes[typeID]
	}
	if opticalAppointmentTypes[typeID] {
		return false
	}
	if crystalRiverAppointmentTypes[typeID] {
		return false
	}

	return true
}

// OfficeCatalog selects environment-specific office policy once at composition.
// Lookups return owned copies; callers cannot mutate the catalog or each other.
type OfficeCatalog struct {
	offices map[string]*OfficeConfig
	aliases map[string]string
}

func NewOfficeCatalog(env string) *OfficeCatalog {
	source := prodOffices
	if env == "dev" {
		source = devOffices
	}
	catalog := &OfficeCatalog{offices: make(map[string]*OfficeConfig), aliases: make(map[string]string, len(source))}
	for phone, office := range source {
		if _, exists := catalog.offices[office.ID]; !exists {
			copy := cloneOffice(office)
			copy.dev = env == "dev"
			catalog.offices[office.ID] = copy
		}
		catalog.aliases[phone] = office.ID
	}
	return catalog
}

func cloneOffice(office *OfficeConfig) *OfficeConfig {
	copy := *office
	copy.Columns = make(map[string]OfficeColumn, len(office.Columns))
	for id, column := range office.Columns {
		column.SameStartWindows = append([]SameStartWindow(nil), column.SameStartWindows...)
		copy.Columns[id] = column
	}
	copy.RoutingTiers = make(map[RoutingRule][]string, len(office.RoutingTiers))
	for rule, columns := range office.RoutingTiers {
		copy.RoutingTiers[rule] = append([]string(nil), columns...)
	}
	return &copy
}

// StripToDigits removes all non-digit characters from a string.
func StripToDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// NormalizePhoneDigits strips a phone number to digits and removes the
// leading US country code ("1") if the result is 11 digits. AMD stores
// 10-digit numbers and won't match on 11.
func NormalizePhoneDigits(s string) string {
	digits := StripToDigits(s)
	if len(digits) == 11 && digits[0] == '1' {
		return digits[1:]
	}
	return digits
}

// LookupOffice resolves a SIP trunk phone number or office name to its office config.
// Phone lookup accepts E.164, 11-digit US, 10-digit US, and formatted US numbers.
func (c *OfficeCatalog) LookupOffice(phone string) (*OfficeConfig, bool) {
	phone = strings.TrimSpace(phone)
	for _, key := range officePhoneLookupKeys(phone) {
		if id, ok := c.aliases[key]; ok {
			return c.LookupOfficeByID(id)
		}
	}

	lookup := normalizeOfficeLookup(phone)
	compactLookup := strings.ReplaceAll(lookup, " ", "")
	for _, office := range c.offices {
		for _, candidate := range []string{office.ID, office.DisplayName} {
			normalized := normalizeOfficeLookup(candidate)
			if lookup == normalized || compactLookup == strings.ReplaceAll(normalized, " ", "") {
				return cloneOffice(office), true
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

	digits := StripToDigits(phone)
	switch {
	case len(digits) == 10:
		keys = append(keys, "+1"+digits)
	case len(digits) == 11 && digits[0] == '1':
		keys = append(keys, "+"+digits)
	}

	return keys
}

// DefaultOffice returns the fallback office config (Spring Hill).
func (c *OfficeCatalog) DefaultOffice() *OfficeConfig {
	return cloneOffice(c.offices["spring_hill"])
}

// ValidOfficeNames returns the list of recognized office display names.
func (c *OfficeCatalog) ValidOfficeNames() []string {
	seen := make(map[string]bool)
	names := make([]string, 0, len(c.offices))
	for _, office := range c.offices {
		if !seen[office.DisplayName] {
			seen[office.DisplayName] = true
			names = append(names, office.DisplayName)
		}
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
