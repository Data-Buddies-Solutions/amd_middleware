package domain

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

type OfficeConfig struct {
	ID               string
	DisplayName      string
	FacilityID       string
	DefaultProfileID string
	Columns          map[string]OfficeColumn
	RoutingTiers     map[RoutingRule][]string
	PediatricRouting RoutingRule
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

func (o *OfficeConfig) ProvidersForRoutingAndDOB(rule RoutingRule, dob string) []string {
	colIDs, ok := o.columnIDsForRouting(rule)
	if !ok {
		return nil
	}
	colMap := o.ColumnsForRoutingAndDOB(rule, dob)
	return o.providerNamesForColumnIDs(colIDs, colMap)
}

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

func (o *OfficeConfig) ProviderDisplayName(profileID string) string {
	for _, col := range o.Columns {
		if col.ProfileID == profileID {
			return col.DisplayName
		}
	}
	return ""
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
	color, ok := DefaultAppointmentTypeColors[typeID]
	return color, ok
}

func (o *OfficeConfig) AppointmentTypeName(typeID int) (string, bool) {
	name, ok := DefaultAppointmentTypeNames[typeID]
	return name, ok
}

func LookupOfficeByID(officeID string) (*OfficeConfig, bool) {
	for _, office := range officeRegistry {
		if office.ID == officeID {
			return office, true
		}
	}
	return nil, false
}

func AppointmentLookupOfficeIDs(office *OfficeConfig) []string {
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
	seen := make(map[*OfficeConfig]bool, len(officeIDs))
	for _, officeID := range officeIDs {
		lookupOffice, ok := LookupOfficeByID(officeID)
		if !ok {
			continue
		}
		if seen[lookupOffice] {
			continue
		}
		seen[lookupOffice] = true
		lookupIDs = append(lookupIDs, lookupOffice.ID)
	}
	if len(lookupIDs) == 0 {
		return []string{office.ID}
	}
	return lookupIDs
}

const PreservedAppointmentTypeFallbackColor = "TEAL"

var DefaultAppointmentTypeColors = map[int]string{
	1006: "RED",
	1004: "GREEN",
	1007: "ORANGE",
	1005: "PINK",
	1008: "BLUE",
	1010: "TEAL",
	3364: "ROSE",
	4244: "BROWN",
	4245: "GRAY",
	6167: "ORANGE",
	6168: "TEAL",
	6169: "RED",
}

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

var devAppointmentTypes = map[int]int{
	1006: 12,
	1004: 20,
	1007: 18,
	1005: 8,
	1008: 1627,
}

var isDevEnv bool

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

func CanonicalAppointmentTypeID(typeID int) (int, bool) {
	if !isDevEnv {
		if _, ok := DefaultAppointmentTypeColors[typeID]; !ok {
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

func (o *OfficeConfig) AllowsAppointmentType(typeID int, routing RoutingRule) bool {
	if _, ok := DefaultAppointmentTypeColors[typeID]; !ok {
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

var springHillOffice = &OfficeConfig{
	ID:               "spring_hill",
	DisplayName:      "Spring Hill",
	FacilityID:       "1568",
	DefaultProfileID: "620",
	Columns: map[string]OfficeColumn{
		"1513": {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1598": {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1551": {ProfileID: "2064", DisplayName: "Dr. Joseph Licht", ShortName: "Dr. Licht", MatchKey: "LICHT", SameStartCapacity: 2},
		"1550": {ProfileID: "2076", DisplayName: "Dr. Noel", ShortName: "Dr. Noel", MatchKey: "NOEL", SameStartCapacity: 2},
		"1600": {ProfileID: "1983", DisplayName: "Dr. Melissa Otero", ShortName: "Dr. Otero", MatchKey: "OTERO"},
	},
	RoutingTiers: map[RoutingRule][]string{
		RoutingBachOnly:    {"1513", "1598"},
		RoutingBachLicht:   {"1513", "1598", "1551"},
		RoutingAll:         {"1513", "1598", "1551", "1550"},
		RoutingOpticalOnly: {"1600"},
	},
	PediatricRouting: RoutingBachOnly,
}

var crystalRiverOffice = &OfficeConfig{
	ID:               "crystal_river",
	DisplayName:      "Crystal River",
	FacilityID:       "1576",
	DefaultProfileID: "2064",
	Columns: map[string]OfficeColumn{
		"1593": {ProfileID: "2064", DisplayName: "Dr. Joseph Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"},
	},
	RoutingTiers: map[RoutingRule][]string{
		RoutingBachOnly:  {"1593"},
		RoutingBachLicht: {"1593"},
		RoutingAll:       {"1593"},
	},
	PediatricRouting: RoutingNotAccepted,
}

var hollywoodSweetwaterRoutineDoubleBookWindows = []SameStartWindow{
	sameStartWindow(time.Monday, 8, 30, 10, 45),
	sameStartWindow(time.Monday, 13, 30, 14, 30),
	sameStartWindow(time.Tuesday, 8, 30, 10, 45),
	sameStartWindow(time.Tuesday, 13, 30, 14, 30),
	sameStartWindow(time.Wednesday, 8, 30, 10, 45),
	sameStartWindow(time.Wednesday, 13, 30, 14, 30),
	sameStartWindow(time.Thursday, 8, 30, 10, 45),
	sameStartWindow(time.Thursday, 13, 30, 14, 30),
	sameStartWindow(time.Friday, 8, 30, 11, 45),
}

var sweetwaterOffice = &OfficeConfig{
	ID:               "sweetwater",
	DisplayName:      "Sweetwater",
	FacilityID:       "670",
	DefaultProfileID: "620",
	Columns: map[string]OfficeColumn{
		"682":  {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1307": {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1296": {
			ProfileID:         "1996",
			DisplayName:       "Dr. Maria Casas",
			ShortName:         "Dr. Casas",
			MatchKey:          "CASAS",
			MinAgeYears:       7,
			SameStartCapacity: 2,
			SameStartWindows:  hollywoodSweetwaterRoutineDoubleBookWindows,
		},
		"1554": {
			ProfileID:         "2075",
			DisplayName:       "Dr. Kyler Farnan",
			ShortName:         "Dr. Farnan",
			MatchKey:          "FARNAN",
			MinAgeYears:       5,
			SameStartCapacity: 2,
			SameStartWindows:  hollywoodSweetwaterRoutineDoubleBookWindows,
		},
		"1210": {
			ProfileID:         "1993",
			DisplayName:       "Dr. Gisselle Calero",
			ShortName:         "Dr. Calero",
			MatchKey:          "CALERO",
			MinAgeYears:       4,
			SameStartCapacity: 2,
			SameStartWindows:  hollywoodSweetwaterRoutineDoubleBookWindows,
		},
	},
	RoutingTiers: map[RoutingRule][]string{
		RoutingBachOnly:    {"682", "1307"},
		RoutingBachLicht:   {"682", "1307"},
		RoutingAll:         {"682", "1307"},
		RoutingOpticalOnly: {"1296", "1554", "1210"},
	},
	PediatricRouting: RoutingBachOnly,
}

var hollywoodOffice = &OfficeConfig{
	ID:               "hollywood",
	DisplayName:      "Hollywood",
	FacilityID:       "1480",
	DefaultProfileID: "620",
	Columns: map[string]OfficeColumn{
		"1268": {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1478": {ProfileID: "620", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1555": {
			ProfileID:         "2075",
			DisplayName:       "Dr. Kyler Farnan",
			ShortName:         "Dr. Farnan",
			MatchKey:          "FARNAN",
			MinAgeYears:       5,
			SameStartCapacity: 2,
			SameStartWindows:  hollywoodSweetwaterRoutineDoubleBookWindows,
		},
		"1510": {
			ProfileID:         "2057",
			DisplayName:       "Dr. Lisbet Vidal",
			ShortName:         "Dr. Vidal",
			MatchKey:          "VIDAL",
			MinAgeYears:       7,
			SameStartCapacity: 2,
			SameStartWindows:  hollywoodSweetwaterRoutineDoubleBookWindows,
		},
		"1305": {
			ProfileID:         "1993",
			DisplayName:       "Dr. Gisselle Calero",
			ShortName:         "Dr. Calero",
			MatchKey:          "CALERO",
			MinAgeYears:       4,
			SameStartCapacity: 2,
			SameStartWindows:  hollywoodSweetwaterRoutineDoubleBookWindows,
		},
	},
	RoutingTiers: map[RoutingRule][]string{
		RoutingBachOnly:    {"1268", "1478"},
		RoutingBachLicht:   {"1268", "1478"},
		RoutingAll:         {"1268", "1478"},
		RoutingOpticalOnly: {"1555", "1510", "1305"},
	},
	PediatricRouting: RoutingBachOnly,
}

var northMiamiBeachOpticalOffice = &OfficeConfig{
	ID:               "north_miami_beach_optical",
	DisplayName:      "North Miami Beach Optical",
	FacilityID:       "1582",
	DefaultProfileID: "621",
	Columns: map[string]OfficeColumn{
		"1601": {ProfileID: "621", DisplayName: "Dr. Miriam Bach", ShortName: "Dr. Miriam Bach", MatchKey: "BACH"},
	},
	RoutingTiers: map[RoutingRule][]string{
		RoutingOpticalOnly: {"1601"},
	},
	PediatricRouting: RoutingNotAccepted,
}

var devSpringHillOffice = &OfficeConfig{
	ID:               "spring_hill",
	DisplayName:      "Spring Hill",
	FacilityID:       "1032",
	DefaultProfileID: "1135",
	Columns: map[string]OfficeColumn{
		"1716": {ProfileID: "1135", DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH", SameStartCapacity: 2},
		"1723": {ProfileID: "1141", DisplayName: "Dr. Joseph Licht", ShortName: "Dr. Licht", MatchKey: "LICHT", SameStartCapacity: 2},
		"1726": {ProfileID: "1137", DisplayName: "Dr. Noel", ShortName: "Dr. Noel", MatchKey: "NOEL", SameStartCapacity: 2},
	},
	RoutingTiers: map[RoutingRule][]string{
		RoutingBachOnly:  {"1716"},
		RoutingBachLicht: {"1716", "1723"},
		RoutingAll:       {"1716", "1723", "1726"},
	},
	PediatricRouting: RoutingBachOnly,
}

var prodOffices = map[string]*OfficeConfig{
	"+17275919997": springHillOffice,
	"+13523202007": crystalRiverOffice,
	"+19542872010": hollywoodOffice,
	"+17864657475": sweetwaterOffice,
	"+17864654845": sweetwaterOffice,
	"+17866134310": sweetwaterOffice,
	"+17864657479": sweetwaterOffice,
	"+17864654836": sweetwaterOffice,
	"+17864654882": sweetwaterOffice,
	"+13055095333": northMiamiBeachOpticalOffice,
}

var devOffices = map[string]*OfficeConfig{
	"+14843989071": devSpringHillOffice,
}

var officeRegistry = prodOffices

var DefaultPhone = "+17275919997"

func InitRegistry(env string) {
	switch env {
	case "dev":
		officeRegistry = devOffices
		DefaultPhone = "+14843989071"
		isDevEnv = true
		log.Printf("Office registry: dev")
	default:
		officeRegistry = prodOffices
		DefaultPhone = "+17275919997"
		isDevEnv = false
		log.Printf("Office registry: prod")
	}
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
		if office, ok := officeRegistry[key]; ok {
			return office, true
		}
	}

	lookup := normalizeOfficeLookup(phone)
	compactLookup := strings.ReplaceAll(lookup, " ", "")
	for _, office := range officeRegistry {
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
	return officeRegistry[DefaultPhone]
}

func validOfficeNames() []string {
	seen := make(map[string]bool)
	names := make([]string, 0, len(officeRegistry))
	for _, office := range officeRegistry {
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

func (o *OfficeConfig) SchedulingRouting(routing RoutingRule, dob string) RoutingRule {
	if routing == RoutingNotAccepted || routing == RoutingOpticalOnly || !IsMinor(dob) {
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
