package domain

import "time"

const defaultOfficeID = "spring_hill"

type officeTable struct {
	Offices      []officeDefinition
	Environments map[Environment]environmentBinding
}

type officeDefinition struct {
	ID                         string
	DisplayName                string
	Columns                    map[string]columnDefinition
	RoutingTiers               map[RoutingRule][]string
	PediatricRouting           RoutingRule
	AppointmentLookupOfficeIDs []string
	AppointmentTypes           OfficeAppointmentTypes
	RoutineVisionMinimumAge    MinimumAge
}

type columnDefinition struct {
	Provider          provider
	MinAgeYears       int
	SameStartCapacity int
	SameStartWindows  []SameStartWindow
}

type provider struct {
	DisplayName string
	ShortName   string
	MatchKey    string
}

type environmentBinding struct {
	Offices                     map[string]officeBinding
	UnavailableOffices          []string
	AppointmentTypes            map[int]int
	UnavailableAppointmentTypes []int
}

type officeBinding struct {
	Phones             []string
	FacilityID         string
	DefaultProfileID   string
	Columns            map[string]columnBinding
	UnavailableColumns []string
}

type columnBinding struct {
	ColumnID  string
	ProfileID string
}

var (
	austinBach     = provider{DisplayName: "Dr. Austin Bach", ShortName: "Dr. Bach", MatchKey: "BACH"}
	josephLicht    = provider{DisplayName: "Dr. Joseph Licht", ShortName: "Dr. Licht", MatchKey: "LICHT"}
	noel           = provider{DisplayName: "Dr. Noel", ShortName: "Dr. Noel", MatchKey: "NOEL"}
	melissaOtero   = provider{DisplayName: "Dr. Melissa Otero", ShortName: "Dr. Otero", MatchKey: "OTERO"}
	mariaCasas     = provider{DisplayName: "Dr. Maria Casas", ShortName: "Dr. Casas", MatchKey: "CASAS"}
	kylerFarnan    = provider{DisplayName: "Dr. Kyler Farnan", ShortName: "Dr. Farnan", MatchKey: "FARNAN"}
	gisselleCalero = provider{DisplayName: "Dr. Gisselle Calero", ShortName: "Dr. Calero", MatchKey: "CALERO"}
	lisbetVidal    = provider{DisplayName: "Dr. Lisbet Vidal", ShortName: "Dr. Vidal", MatchKey: "VIDAL"}
	miriamBach     = provider{DisplayName: "Dr. Miriam Bach", ShortName: "Dr. Miriam Bach", MatchKey: "BACH"}
)

var AppointmentTypes = map[int]AppointmentType{
	1004: {Name: "New Pediatric Medical", Color: "GREEN", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgePediatric},
	1005: {Name: "Established Pediatric Medical (Follow Up)", Color: "PINK", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgePediatric},
	1006: {Name: "New Adult Medical", Color: "RED", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAdult},
	1007: {Name: "Established Adult Medical (Follow Up)", Color: "ORANGE", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAdult},
	1008: {Name: "Post Op", Color: "BLUE", VisitType: AppointmentVisitMedical, AgeBand: appointmentAgeAny},
	1010: {Name: "New Adult Vision", Color: "TEAL", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgeAdult},
	3364: {Name: "Established Adult Vision", Color: "ROSE", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgeAdult},
	4244: {Name: "New Pediatric Vision", Color: "BROWN", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgePediatric},
	4245: {Name: "Established Pediatric Vision", Color: "GRAY", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgePediatric},
	6167: {Name: "Crystal River New Patient", Color: "ORANGE", VisitType: AppointmentVisitMedical, AgeBand: appointmentAgeAny},
	6168: {Name: "Crystal River Post Op", Color: "TEAL", VisitType: AppointmentVisitMedical, AgeBand: appointmentAgeAny},
	6169: {Name: "Crystal River Established Patient", Color: "RED", VisitType: AppointmentVisitMedical, AgeBand: appointmentAgeAny},
}

var standardAppointmentTypes = OfficeAppointmentTypes{
	PostOp:        1008,
	Medical:       AppointmentTypeLadder{NewPediatric: 1004, NewAdult: 1006, EstablishedPediatric: 1005, EstablishedAdult: 1007},
	RoutineVision: AppointmentTypeLadder{NewPediatric: 4244, NewAdult: 1010, EstablishedPediatric: 4245, EstablishedAdult: 3364},
}

var crystalRiverAppointmentTypes = OfficeAppointmentTypes{
	PostOp:  6168,
	Medical: AppointmentTypeLadder{NewPediatric: 6167, NewAdult: 6167, EstablishedPediatric: 6169, EstablishedAdult: 6169},
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

func newOfficeTable() officeTable {
	return officeTable{
		Offices: []officeDefinition{
			{
				ID:          "spring_hill",
				DisplayName: "Spring Hill",
				Columns: map[string]columnDefinition{
					"bach":   {Provider: austinBach, SameStartCapacity: 2},
					"bach_2": {Provider: austinBach, SameStartCapacity: 2},
					"licht":  {Provider: josephLicht, SameStartCapacity: 2},
					"noel":   {Provider: noel, SameStartCapacity: 2},
					"otero":  {Provider: melissaOtero},
				},
				RoutingTiers: map[RoutingRule][]string{
					RoutingBachOnly:    {"bach", "bach_2"},
					RoutingBachLicht:   {"bach", "bach_2", "licht"},
					RoutingAll:         {"bach", "bach_2", "licht", "noel"},
					RoutingOpticalOnly: {"otero"},
				},
				PediatricRouting:           RoutingBachOnly,
				AppointmentLookupOfficeIDs: []string{"spring_hill", "crystal_river"},
				AppointmentTypes:           standardAppointmentTypes,
				RoutineVisionMinimumAge: MinimumAge{
					Years:   7,
					Message: "Spring Hill does not schedule routine vision for children under 7. Search medical availability instead; Dr. Bach sees these children.",
				},
			},
			{
				ID:          "crystal_river",
				DisplayName: "Crystal River",
				Columns: map[string]columnDefinition{
					"licht": {Provider: josephLicht},
				},
				RoutingTiers: map[RoutingRule][]string{
					RoutingBachOnly:  {"licht"},
					RoutingBachLicht: {"licht"},
					RoutingAll:       {"licht"},
				},
				PediatricRouting:           RoutingNotAccepted,
				AppointmentLookupOfficeIDs: []string{"spring_hill", "crystal_river"},
				AppointmentTypes:           crystalRiverAppointmentTypes,
			},
			{
				ID:          "hollywood",
				DisplayName: "Hollywood",
				Columns: map[string]columnDefinition{
					"bach":   {Provider: austinBach, SameStartCapacity: 2},
					"bach_2": {Provider: austinBach, SameStartCapacity: 2},
					"farnan": {Provider: kylerFarnan, MinAgeYears: 5, SameStartCapacity: 2, SameStartWindows: hollywoodSweetwaterRoutineDoubleBookWindows},
					"vidal":  {Provider: lisbetVidal, MinAgeYears: 7, SameStartCapacity: 2, SameStartWindows: hollywoodSweetwaterRoutineDoubleBookWindows},
					"calero": {Provider: gisselleCalero, MinAgeYears: 4, SameStartCapacity: 2, SameStartWindows: hollywoodSweetwaterRoutineDoubleBookWindows},
				},
				RoutingTiers: map[RoutingRule][]string{
					RoutingBachOnly:    {"bach", "bach_2"},
					RoutingBachLicht:   {"bach", "bach_2"},
					RoutingAll:         {"bach", "bach_2"},
					RoutingOpticalOnly: {"farnan", "vidal", "calero"},
				},
				PediatricRouting:           RoutingBachOnly,
				AppointmentLookupOfficeIDs: []string{"hollywood", "sweetwater", "north_miami_beach_optical"},
				AppointmentTypes:           standardAppointmentTypes,
			},
			{
				ID:          "sweetwater",
				DisplayName: "Sweetwater",
				Columns: map[string]columnDefinition{
					"bach":   {Provider: austinBach, SameStartCapacity: 2},
					"bach_2": {Provider: austinBach, SameStartCapacity: 2},
					"casas":  {Provider: mariaCasas, MinAgeYears: 7, SameStartCapacity: 2, SameStartWindows: hollywoodSweetwaterRoutineDoubleBookWindows},
					"farnan": {Provider: kylerFarnan, MinAgeYears: 5, SameStartCapacity: 2, SameStartWindows: hollywoodSweetwaterRoutineDoubleBookWindows},
					"calero": {Provider: gisselleCalero, MinAgeYears: 4, SameStartCapacity: 2, SameStartWindows: hollywoodSweetwaterRoutineDoubleBookWindows},
				},
				RoutingTiers: map[RoutingRule][]string{
					RoutingBachOnly:    {"bach", "bach_2"},
					RoutingBachLicht:   {"bach", "bach_2"},
					RoutingAll:         {"bach", "bach_2"},
					RoutingOpticalOnly: {"casas", "farnan", "calero"},
				},
				PediatricRouting:           RoutingBachOnly,
				AppointmentLookupOfficeIDs: []string{"hollywood", "sweetwater"},
				AppointmentTypes:           standardAppointmentTypes,
			},
			{
				ID:          "north_miami_beach_optical",
				DisplayName: "North Miami Beach Optical",
				Columns: map[string]columnDefinition{
					"miriam_bach": {Provider: miriamBach},
				},
				RoutingTiers: map[RoutingRule][]string{
					RoutingOpticalOnly: {"miriam_bach"},
				},
				PediatricRouting:           RoutingNotAccepted,
				AppointmentLookupOfficeIDs: []string{"north_miami_beach_optical"},
				AppointmentTypes:           standardAppointmentTypes,
			},
		},
		Environments: map[Environment]environmentBinding{
			Prod: {
				Offices: map[string]officeBinding{
					"spring_hill": {
						Phones:           []string{"+17275919997"},
						FacilityID:       "1568",
						DefaultProfileID: "620",
						Columns: map[string]columnBinding{
							"bach":   {ColumnID: "1513", ProfileID: "620"},
							"bach_2": {ColumnID: "1598", ProfileID: "620"},
							"licht":  {ColumnID: "1551", ProfileID: "2064"},
							"noel":   {ColumnID: "1550", ProfileID: "2076"},
							"otero":  {ColumnID: "1600", ProfileID: "1983"},
						},
					},
					"crystal_river": {
						Phones:           []string{"+13523202007"},
						FacilityID:       "1576",
						DefaultProfileID: "2064",
						Columns: map[string]columnBinding{
							"licht": {ColumnID: "1593", ProfileID: "2064"},
						},
					},
					"hollywood": {
						Phones:           []string{"+19542872010"},
						FacilityID:       "1480",
						DefaultProfileID: "620",
						Columns: map[string]columnBinding{
							"bach":   {ColumnID: "1268", ProfileID: "620"},
							"bach_2": {ColumnID: "1478", ProfileID: "620"},
							"farnan": {ColumnID: "1555", ProfileID: "2075"},
							"vidal":  {ColumnID: "1510", ProfileID: "2057"},
							"calero": {ColumnID: "1305", ProfileID: "1993"},
						},
					},
					"sweetwater": {
						Phones:           []string{"+17864657475", "+17864654845", "+17866134310", "+17864657479", "+17864654836", "+17864654882"},
						FacilityID:       "670",
						DefaultProfileID: "620",
						Columns: map[string]columnBinding{
							"bach":   {ColumnID: "682", ProfileID: "620"},
							"bach_2": {ColumnID: "1307", ProfileID: "620"},
							"casas":  {ColumnID: "1296", ProfileID: "1996"},
							"farnan": {ColumnID: "1554", ProfileID: "2075"},
							"calero": {ColumnID: "1210", ProfileID: "1993"},
						},
					},
					"north_miami_beach_optical": {
						Phones:           []string{"+13055095333"},
						FacilityID:       "1582",
						DefaultProfileID: "621",
						Columns: map[string]columnBinding{
							"miriam_bach": {ColumnID: "1601", ProfileID: "621"},
						},
					},
				},
				AppointmentTypes: map[int]int{
					1004: 1004, 1005: 1005, 1006: 1006, 1007: 1007, 1008: 1008,
					1010: 1010, 3364: 3364, 4244: 4244, 4245: 4245,
					6167: 6167, 6168: 6168, 6169: 6169,
				},
			},
			Sandbox: {
				Offices: map[string]officeBinding{
					"spring_hill": {
						Phones:           []string{"+14843989071"},
						FacilityID:       "1032",
						DefaultProfileID: "1135",
						Columns: map[string]columnBinding{
							"bach":  {ColumnID: "1716", ProfileID: "1135"},
							"licht": {ColumnID: "1723", ProfileID: "1141"},
							"noel":  {ColumnID: "1726", ProfileID: "1137"},
						},
						UnavailableColumns: []string{"bach_2", "otero"},
					},
				},
				UnavailableOffices: []string{"crystal_river", "hollywood", "sweetwater", "north_miami_beach_optical"},
				AppointmentTypes: map[int]int{
					1004: 20, 1005: 8, 1006: 12, 1007: 18, 1008: 1627,
				},
				UnavailableAppointmentTypes: []int{1010, 3364, 4244, 4245, 6167, 6168, 6169},
			},
		},
	}
}
