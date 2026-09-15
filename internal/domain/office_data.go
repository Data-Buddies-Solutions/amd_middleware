package domain

import (
	"time"
)

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

// prodOffices contains office configs keyed by SIP trunk phone number (E.164).
var prodOffices = map[string]*OfficeConfig{
	"+17275919997": springHillOffice,
	"+13523202007": crystalRiverOffice,
	// TODO: clean up — placeholder number for Crystal River, duplicates config above
	"+16182265883": crystalRiverOffice,
	"+19542872010": hollywoodOffice,
	"+17864657475": sweetwaterOffice,
	"+17864654845": sweetwaterOffice,
	"+17866134310": sweetwaterOffice,
	"+17864657479": sweetwaterOffice,
	"+17864654836": sweetwaterOffice,
	"+17864654882": sweetwaterOffice,
	"+13055095333": northMiamiBeachOpticalOffice,
}

// devOffices contains office configs keyed by SIP trunk phone number (E.164).
var devOffices = map[string]*OfficeConfig{
	"+17275919997": devSpringHillOffice,
	"+14843989071": devSpringHillOffice,
}
