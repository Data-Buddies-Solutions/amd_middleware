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

	appointmentAgeAny       = "any"
	AppointmentAgeAdult     = "adult"
	AppointmentAgePediatric = "pediatric"
)

type AppointmentType struct {
	Name      string
	Color     string
	VisitType string
	AgeBand   string
}

type OfficeAppointmentTypes struct {
	PostOp        int
	Medical       AppointmentTypeLadder
	RoutineVision AppointmentTypeLadder
}

type AppointmentTypeLadder struct {
	NewPediatric         int
	NewAdult             int
	EstablishedPediatric int
	EstablishedAdult     int
}

func (l AppointmentTypeLadder) TypeFor(patientStatus, ageBand string) int {
	if patientStatus == AppointmentPatientNew {
		if ageBand == AppointmentAgePediatric {
			return l.NewPediatric
		}
		return l.NewAdult
	}
	if ageBand == AppointmentAgePediatric {
		return l.EstablishedPediatric
	}
	return l.EstablishedAdult
}

func (l AppointmentTypeLadder) includes(typeID int) bool {
	return typeID == l.NewPediatric || typeID == l.NewAdult || typeID == l.EstablishedPediatric || typeID == l.EstablishedAdult
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

func ValidVisitType(visit string) bool {
	return visit == AppointmentVisitMedical || visit == AppointmentVisitRoutineVision
}

func VisitRouting(visit string) RoutingRule {
	if visit == AppointmentVisitRoutineVision {
		return RoutingOpticalOnly
	}
	return RoutingAll
}

func (o *OfficeConfig) SupportsVisit(visit string) bool {
	if visit == AppointmentVisitMedical {
		return o.SupportsMedical()
	}
	return o.SupportsRouting(RoutingOpticalOnly)
}

func AppointmentVisitType(typeID int) string {
	return AppointmentTypes[typeID].VisitType
}
