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
	DOB           string
	VisitReason   string
}

type appointmentTypeResolution struct {
	AppointmentTypeID int
	Missing           []string
	Message           string
}

func resolveAppointmentTypeForIntent(office *domain.OfficeConfig, routing domain.RoutingRule, intent appointmentIntent, asOf time.Time) appointmentTypeResolution {
	if office == nil {
		return unresolvedAppointmentType([]string{"office"}, "Office is required to resolve appointment type.")
	}

	postOp := appointmentReasonLooksPostOp(intent.VisitReason)
	category := intent.VisitCategory
	status := intent.PatientStatus
	ageBand := appointmentAgeBand(intent.DOB, asOf)

	if postOp {
		if routing == domain.RoutingOpticalOnly {
			return unresolvedAppointmentType([]string{"routing"}, "Post-op appointments must use a medical scheduling lane.")
		}
		return resolvedAppointmentType(office.AppointmentTypes.PostOp)
	}

	if category == domain.AppointmentVisitRoutineVision {
		if len(office.ColumnsForRouting(domain.RoutingOpticalOnly)) == 0 {
			return unresolvedAppointmentType([]string{"routeToSpringHill"}, fmt.Sprintf("Routine vision scheduling is not supported at %s. Route the visit to Spring Hill before booking.", office.DisplayName))
		}

		ladder := office.AppointmentTypes.RoutineVision
		missing := missingAppointmentTypeFacts(status, ageBand, ladder.DependsOnAge())
		if len(missing) > 0 {
			return unresolvedAppointmentType(missing, appointmentTypeMissingFactsMessage(missing))
		}
		if ageBand == domain.AppointmentAgePediatric {
			if age, _ := domain.AgeYearsOn(intent.DOB, asOf); routineVisionTooYoung(office, age) {
				return unresolvedAppointmentType([]string{"appointmentLane"}, office.RoutineVisionMinimumAge.Message)
			}
		}
		return resolvedAppointmentType(ladder.TypeFor(status, ageBand))
	}

	if len(office.ColumnsForRouting(routing)) == 0 {
		return unresolvedAppointmentType([]string{"routing"}, fmt.Sprintf("Medical scheduling is not supported at %s.", office.DisplayName))
	}

	ladder := office.AppointmentTypes.Medical
	missing := missingAppointmentTypeFacts(status, ageBand, ladder.DependsOnAge())
	if len(missing) > 0 {
		return unresolvedAppointmentType(missing, appointmentTypeMissingFactsMessage(missing))
	}
	return resolvedAppointmentType(ladder.TypeFor(status, ageBand))
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

func missingAppointmentTypeFacts(status, ageBand string, needsAge bool) []string {
	var missing []string
	if status == "" {
		missing = append(missing, "patientStatus")
	}
	if needsAge && ageBand == "" {
		missing = append(missing, "dob")
	}
	return missing
}

func appointmentTypeMissingFactsMessage(missing []string) string {
	needsStatus := false
	needsDOB := false
	for _, field := range missing {
		switch field {
		case "patientStatus":
			needsStatus = true
		case "dob":
			needsDOB = true
		}
	}

	switch {
	case needsStatus && needsDOB:
		return "Confirm whether this is a new or established patient and verify DOB before booking."
	case needsStatus:
		return "Confirm whether this is a new or established patient before booking."
	case needsDOB:
		return "Verify the patient's DOB before booking."
	default:
		return "More appointment details are required before booking."
	}
}

func appointmentAgeBand(dob string, asOf time.Time) string {
	if age, ok := domain.AgeYearsOn(dob, asOf); ok {
		if age < 18 {
			return domain.AppointmentAgePediatric
		}
		return domain.AppointmentAgeAdult
	}
	return ""
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
