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

func (l AppointmentTypeLadder) DependsOnAge() bool {
	return l.NewPediatric != l.NewAdult || l.EstablishedPediatric != l.EstablishedAdult
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

func AppointmentVisitType(typeID int) string {
	return AppointmentTypes[typeID].VisitType
}
