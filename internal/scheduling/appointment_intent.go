package scheduling

import (
	"fmt"
	"strings"
	"time"

	"advancedmd-token-management/internal/domain"
)

type appointmentIntent struct {
	VisitCategory string
	PatientStatus string
	VisitReason   string
}

type appointmentTypeResolution struct {
	AppointmentTypeID int
	Missing           []string
	Message           string
}

func resolveAppointmentTypeForIntent(office *domain.OfficeConfig, routing domain.RoutingRule, intent appointmentIntent, dob domain.DOB, asOf time.Time) appointmentTypeResolution {
	if office == nil {
		return unresolvedAppointmentType([]string{"office"}, "Office is required to resolve appointment type.")
	}

	ageBand := dob.AgeBandOn(asOf)

	if appointmentReasonLooksPostOp(intent.VisitReason) {
		if routing == domain.RoutingOpticalOnly {
			return unresolvedAppointmentType([]string{"routing"}, "Post-op appointments must use a medical scheduling lane.")
		}
		return resolvedAppointmentType(office.AppointmentTypes.PostOp)
	}

	if intent.VisitCategory == domain.AppointmentVisitRoutineVision {
		if len(office.ColumnsForRouting(domain.RoutingOpticalOnly)) == 0 {
			return unresolvedAppointmentType([]string{"routeToSpringHill"}, fmt.Sprintf("Routine vision scheduling is not supported at %s. Route the visit to Spring Hill before booking.", office.DisplayName))
		}
		if age, _ := dob.AgeOn(asOf); ageBand == domain.AppointmentAgePediatric && routineVisionTooYoung(office, age) {
			return unresolvedAppointmentType([]string{"appointmentLane"}, office.RoutineVisionMinimumAge.Message)
		}
		return resolvedAppointmentType(office.AppointmentTypes.RoutineVision.TypeFor(intent.PatientStatus, ageBand))
	}

	if len(office.ColumnsForRouting(routing)) == 0 {
		return unresolvedAppointmentType([]string{"routing"}, fmt.Sprintf("Medical scheduling is not supported at %s.", office.DisplayName))
	}
	return resolvedAppointmentType(office.AppointmentTypes.Medical.TypeFor(intent.PatientStatus, ageBand))
}

func routineVisionTooYoung(office *domain.OfficeConfig, age int) bool {
	return age < office.RoutineVisionMinimumAge.Years
}

func resolvedAppointmentType(typeID int) appointmentTypeResolution {
	return appointmentTypeResolution{AppointmentTypeID: typeID}
}

func unresolvedAppointmentType(missing []string, message string) appointmentTypeResolution {
	return appointmentTypeResolution{
		Missing: missing,
		Message: message,
	}
}

func appointmentReasonLooksPostOp(reason string) bool {
	normalized := normalizeAppointmentToken(reason)
	return strings.Contains(normalized, "post op") ||
		strings.Contains(normalized, "post operative") ||
		strings.Contains(normalized, "postoperative") ||
		strings.Contains(normalized, "surgery follow up") ||
		strings.Contains(normalized, "recent surgery")
}

func normalizeAppointmentToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.Join(strings.Fields(value), " ")
	return value
}
