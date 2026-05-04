package domain

import (
	"log"
	"strings"
)

// OfficeConfig defines the configuration for a single office location.
type OfficeConfig struct {
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
	ProfileID   string // "620"
	DisplayName string // "Dr. Austin Bach"
	ShortName   string // "Dr. Bach"
	MatchKey    string // "BACH" — uppercase fragment for matching AMD names
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
	if rule == RoutingNotAccepted {
		return nil
	}

	colIDs, ok := o.RoutingTiers[rule]
	if !ok {
		if rule == RoutingOpticalOnly {
			return map[string]bool{}
		}
		// Fall back to all columns for this office
		colIDs = o.RoutingTiers[RoutingAll]
	}

	result := make(map[string]bool, len(colIDs))
	for _, id := range colIDs {
		result[id] = true
	}
	return result
}

// ProvidersForRouting returns the display names for a routing rule at this office.
func (o *OfficeConfig) ProvidersForRouting(rule RoutingRule) []string {
	if rule == RoutingNotAccepted {
		return nil
	}

	colIDs, ok := o.RoutingTiers[rule]
	if !ok {
		if rule == RoutingOpticalOnly {
			return []string{}
		}
		colIDs = o.RoutingTiers[RoutingAll]
	}

	names := make([]string, 0, len(colIDs))
	for _, id := range colIDs {
		if col, ok := o.Columns[id]; ok {
			names = append(names, col.ShortName)
		}
	}
	return names
}

// ValidProviderNames returns all provider short names for this office.
func (o *OfficeConfig) ValidProviderNames() []string {
	names := make([]string, 0, len(o.Columns))
	for _, col := range o.Columns {
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
			if match == "" ||
				(strings.Contains(match, "Overflow") && !strings.Contains(col.DisplayName, "Overflow")) ||
				len(col.DisplayName) < len(match) {
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
	color, ok := DefaultAppointmentTypeColors[typeID]
	return color, ok
}

// AppointmentTypeName returns the friendly name for an appointment type ID.
func (o *OfficeConfig) AppointmentTypeName(typeID int) (string, bool) {
	name, ok := DefaultAppointmentTypeNames[typeID]
	return name, ok
}

// DefaultAppointmentTypeColors maps AMD appointment type IDs to booking colors.
var DefaultAppointmentTypeColors = map[int]string{
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

// DefaultAppointmentTypeNames maps AMD appointment type IDs to friendly names.
var DefaultAppointmentTypeNames = map[int]string{
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
	1010: 1010, // New Adult Vision (no separate dev ID configured)
	3364: 3364, // Established Adult Vision (no separate dev ID configured)
	4244: 4244, // New Pediatric Vision (no separate dev ID configured)
	4245: 4245, // Established Pediatric Vision (no separate dev ID configured)
	6167: 6167, // Crystal River New Patient (prod IDs used in dev CR placeholder)
	6168: 6168, // Crystal River Post Op (prod IDs used in dev CR placeholder)
	6169: 6169, // Crystal River Established Patient (prod IDs used in dev CR placeholder)
}

// isDevEnv tracks whether we're running in dev mode. Set by InitRegistry.
var isDevEnv bool

// ResolveAppointmentTypeID translates a prod type ID to the env-specific ID.
// In prod, returns the ID unchanged. In dev, maps to the dev ID.
func ResolveAppointmentTypeID(typeID int) (int, bool) {
	if _, ok := DefaultAppointmentTypeColors[typeID]; !ok {
		return 0, false
	}
	if isDevEnv {
		devID, ok := devAppointmentTypes[typeID]
		return devID, ok
	}
	return typeID, true
}

// AllowsAppointmentType reports whether an appointment type can be booked for this office/routing lane.
func (o *OfficeConfig) AllowsAppointmentType(typeID int, routing RoutingRule) bool {
	if _, ok := DefaultAppointmentTypeColors[typeID]; !ok {
		return false
	}

	if routing == RoutingOpticalOnly {
		return o.ID == "spring_hill" && opticalAppointmentTypes[typeID]
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

// prodOffices contains office configs keyed by SIP trunk phone number (E.164).
var prodOffices = map[string]*OfficeConfig{
	"+17275919997": {
		ID:               "spring_hill",
		DisplayName:      "Spring Hill",
		FacilityID:       "1568",
		DefaultProfileID: "620",
		Columns: map[string]OfficeColumn{
			"1513": {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH"},
			"1598": {ProfileID: "620", DisplayName: "Dr. Austin Bach (Overflow)", ShortName: "Dr. Bach", MatchKey: "BACH"},
			"1551": {ProfileID: "2064", DisplayName: "Dr. J. Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"},
			"1550": {ProfileID: "2076", DisplayName: "Dr. D. Noel", ShortName: "Dr. Noel", MatchKey: "NOEL"},
			"1600": {ProfileID: "1983", DisplayName: "Routine Vision - Dr. Melissa Otero", ShortName: "Routine Vision", MatchKey: "OTERO"},
		},
		RoutingTiers: map[RoutingRule][]string{
			RoutingBachOnly:    {"1513", "1598"},
			RoutingBachLicht:   {"1513", "1598", "1551"},
			RoutingAll:         {"1513", "1598", "1551", "1550"},
			RoutingOpticalOnly: {"1600"},
		},
		PediatricRouting: RoutingBachOnly,
	},
	"+13523202007": {
		ID:               "crystal_river",
		DisplayName:      "Crystal River",
		FacilityID:       "1576",
		DefaultProfileID: "2064",
		Columns: map[string]OfficeColumn{
			"1593": {ProfileID: "2064", DisplayName: "Dr. J. Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"},
		},
		RoutingTiers: map[RoutingRule][]string{
			RoutingBachOnly:  {"1593"},
			RoutingBachLicht: {"1593"},
			RoutingAll:       {"1593"},
		},
		PediatricRouting: RoutingNotAccepted,
	},
	// TODO: clean up — placeholder number for Crystal River, duplicates config above
	"+16182265883": {
		ID:               "crystal_river",
		DisplayName:      "Crystal River",
		FacilityID:       "1576",
		DefaultProfileID: "2064",
		Columns: map[string]OfficeColumn{
			"1593": {ProfileID: "2064", DisplayName: "Dr. J. Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"},
		},
		RoutingTiers: map[RoutingRule][]string{
			RoutingBachOnly:  {"1593"},
			RoutingBachLicht: {"1593"},
			RoutingAll:       {"1593"},
		},
		PediatricRouting: RoutingNotAccepted,
	},
}

// devOffices contains office configs keyed by SIP trunk phone number (E.164).
var devOffices = map[string]*OfficeConfig{
	"+14843989071": {
		ID:               "spring_hill",
		DisplayName:      "Spring Hill",
		FacilityID:       "1032",
		DefaultProfileID: "1135",
		Columns: map[string]OfficeColumn{
			"1716": {ProfileID: "1135", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH"},
			"1723": {ProfileID: "1141", DisplayName: "Dr. J. Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"},
			"1726": {ProfileID: "1137", DisplayName: "Dr. D. Noel", ShortName: "Dr. Noel", MatchKey: "NOEL"},
		},
		RoutingTiers: map[RoutingRule][]string{
			RoutingBachOnly:  {"1716"},
			RoutingBachLicht: {"1716", "1723"},
			RoutingAll:       {"1716", "1723", "1726"},
		},
		PediatricRouting: RoutingBachOnly,
	},
	// TODO: clean up — placeholder number for Crystal River, uses prod IDs
	"+16182265883": {
		ID:               "crystal_river",
		DisplayName:      "Crystal River",
		FacilityID:       "1576",
		DefaultProfileID: "2064",
		Columns: map[string]OfficeColumn{
			"1593": {ProfileID: "2064", DisplayName: "Dr. J. Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"},
		},
		RoutingTiers: map[RoutingRule][]string{
			RoutingBachOnly:  {"1593"},
			RoutingBachLicht: {"1593"},
			RoutingAll:       {"1593"},
		},
		PediatricRouting: RoutingNotAccepted,
	},
}

// OfficeRegistry maps SIP trunk phone numbers (E.164) to office configurations.
// Defaults to prod; call InitRegistry to switch environments.
var OfficeRegistry = prodOffices

// DefaultPhone is the fallback phone key when no office is specified in a request.
// Updated by InitRegistry to match the active environment.
var DefaultPhone = "+17275919997"

// InitRegistry sets the active office registry based on the AMD_ENV value.
// "dev" loads dev AMD IDs; anything else (including empty) loads prod.
func InitRegistry(env string) {
	switch env {
	case "dev":
		OfficeRegistry = devOffices
		DefaultPhone = "+14843989071"
		isDevEnv = true
		log.Printf("Office registry: dev")
	default:
		OfficeRegistry = prodOffices
		DefaultPhone = "+17275919997"
		isDevEnv = false
		log.Printf("Office registry: prod")
	}
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

// LookupOffice resolves a SIP trunk phone number (E.164) to its office config.
// Accepts with or without the "+" prefix (e.g. "14843989071" or "+14843989071").
func LookupOffice(phone string) (*OfficeConfig, bool) {
	office, ok := OfficeRegistry[phone]
	if !ok && len(phone) > 0 && phone[0] != '+' {
		office, ok = OfficeRegistry["+"+phone]
	}
	return office, ok
}

// DefaultOffice returns the fallback office config (Spring Hill).
func DefaultOffice() *OfficeConfig {
	return OfficeRegistry[DefaultPhone]
}

// ValidOfficeNames returns the list of recognized office display names.
func ValidOfficeNames() []string {
	names := make([]string, 0, len(OfficeRegistry))
	for _, office := range OfficeRegistry {
		names = append(names, office.DisplayName)
	}
	return names
}
