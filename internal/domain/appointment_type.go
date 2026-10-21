package domain

import (
	"sort"
	"strconv"
	"strings"
)

const (
	AppointmentVisitMedical       = "medical"
	AppointmentVisitRoutineVision = "routine_vision"

	AppointmentPatientNew         = "new"
	AppointmentPatientEstablished = "established"

	AppointmentAgeAny       = "any"
	AppointmentAgeAdult     = "adult"
	AppointmentAgePediatric = "pediatric"
)

type AppointmentType struct {
	Name              string
	Color             string
	VisitType         string
	AgeBand           string
	CrystalRiver      bool
	DevelopmentTypeID int
}

var AppointmentTypes = map[int]AppointmentType{
	1004: {Name: "New Pediatric Medical", Color: "GREEN", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgePediatric, DevelopmentTypeID: 20},
	1005: {Name: "Established Pediatric Medical (Follow Up)", Color: "PINK", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgePediatric, DevelopmentTypeID: 8},
	1006: {Name: "New Adult Medical", Color: "RED", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAdult, DevelopmentTypeID: 12},
	1007: {Name: "Established Adult Medical (Follow Up)", Color: "ORANGE", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAdult, DevelopmentTypeID: 18},
	1008: {Name: "Post Op", Color: "BLUE", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAny, DevelopmentTypeID: 1627},
	1010: {Name: "New Adult Vision", Color: "TEAL", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgeAdult},
	3364: {Name: "Established Adult Vision", Color: "ROSE", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgeAdult},
	4244: {Name: "New Pediatric Vision", Color: "BROWN", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgePediatric},
	4245: {Name: "Established Pediatric Vision", Color: "GRAY", VisitType: AppointmentVisitRoutineVision, AgeBand: AppointmentAgePediatric},
	6167: {Name: "Crystal River New Patient", Color: "ORANGE", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAny, CrystalRiver: true},
	6168: {Name: "Crystal River Post Op", Color: "TEAL", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAny, CrystalRiver: true},
	6169: {Name: "Crystal River Established Patient", Color: "RED", VisitType: AppointmentVisitMedical, AgeBand: AppointmentAgeAny, CrystalRiver: true},
}

func ValidAppointmentTypeIDs() string {
	ids := make([]int, 0, len(AppointmentTypes))
	for id := range AppointmentTypes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.Itoa(id)
	}
	return strings.Join(values, ", ")
}

func AppointmentVisitType(typeID int) string {
	return AppointmentTypes[typeID].VisitType
}
