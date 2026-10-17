package scheduling

import (
	"fmt"
	"strings"

	"advancedmd-token-management/internal/domain"
)

type appointmentIntent struct {
	VisitCategory string
	PatientStatus string
	DOB           string
	VisitReason   string
}

type appointmentTypeResolution struct {
	AppointmentTypeID   int
	AppointmentTypeName string
	Missing             []string
	Message             string
}

func resolveAppointmentTypeForIntent(office *domain.OfficeConfig, routing domain.RoutingRule, intent appointmentIntent) appointmentTypeResolution {
	if office == nil {
		return unresolvedAppointmentType([]string{"office"}, "Office is required to resolve appointment type.")
	}

	postOp := appointmentReasonLooksPostOp(intent.VisitReason)
	category := intent.VisitCategory
	status := intent.PatientStatus
	ageBand := appointmentAgeBand(intent.DOB)

	if postOp {
		if routing == domain.RoutingOpticalOnly {
			return unresolvedAppointmentType([]string{"routing"}, "Post-op appointments must use a medical scheduling lane.")
		}
		if office.ID == "crystal_river" {
			return resolvedAppointmentType(6168)
		}
		return resolvedAppointmentType(1008)
	}

	if category == domain.AppointmentVisitRoutineVision {
		if len(office.ColumnsForRouting(domain.RoutingOpticalOnly)) == 0 {
			return unresolvedAppointmentType([]string{"routeToSpringHill"}, fmt.Sprintf("Routine vision scheduling is not supported at %s. Route the visit to Spring Hill before booking.", office.DisplayName))
		}

		missing := missingAppointmentTypeFacts(status, ageBand)
		if len(missing) > 0 {
			return unresolvedAppointmentType(missing, appointmentTypeMissingFactsMessage(missing))
		}
		if office.ID == "spring_hill" && ageBand == domain.AppointmentAgePediatric {
			age, ok := domain.AgeYears(intent.DOB)
			if !ok {
				return unresolvedAppointmentType([]string{"dob"}, "Patient DOB is required before scheduling pediatric routine vision at Spring Hill.")
			}
			if routineVisionTooYoung(office, age) {
				return unresolvedAppointmentType([]string{"appointmentLane"}, underSevenRoutineVisionMessage)
			}
		}

		if status == domain.AppointmentPatientNew {
			if ageBand == domain.AppointmentAgePediatric {
				return resolvedAppointmentType(4244)
			}
			return resolvedAppointmentType(1010)
		}
		if ageBand == domain.AppointmentAgePediatric {
			return resolvedAppointmentType(4245)
		}
		return resolvedAppointmentType(3364)
	}

	if len(office.ColumnsForRouting(routing)) == 0 {
		return unresolvedAppointmentType([]string{"routing"}, fmt.Sprintf("Medical scheduling is not supported at %s.", office.DisplayName))
	}

	if office.ID == "crystal_river" {
		if status == "" {
			return unresolvedAppointmentType([]string{"patientStatus"}, appointmentTypeMissingFactsMessage([]string{"patientStatus"}))
		}
		if status == domain.AppointmentPatientNew {
			return resolvedAppointmentType(6167)
		}
		return resolvedAppointmentType(6169)
	}

	missing := missingAppointmentTypeFacts(status, ageBand)
	if len(missing) > 0 {
		return unresolvedAppointmentType(missing, appointmentTypeMissingFactsMessage(missing))
	}

	if status == domain.AppointmentPatientNew {
		if ageBand == domain.AppointmentAgePediatric {
			return resolvedAppointmentType(1004)
		}
		return resolvedAppointmentType(1006)
	}
	if ageBand == domain.AppointmentAgePediatric {
		return resolvedAppointmentType(1005)
	}
	return resolvedAppointmentType(1007)
}

const underSevenRoutineVisionMessage = "Spring Hill does not schedule routine vision for children under 7. Search medical availability instead; Dr. Bach sees these children."

func routineVisionTooYoung(office *domain.OfficeConfig, age int) bool {
	return office.ID == "spring_hill" && age < 7
}

func resolvedAppointmentType(typeID int) appointmentTypeResolution {
	return appointmentTypeResolution{
		AppointmentTypeID:   typeID,
		AppointmentTypeName: domain.AppointmentTypes[typeID].Name,
	}
}

func unresolvedAppointmentType(missing []string, message string) appointmentTypeResolution {
	return appointmentTypeResolution{
		Missing: missing,
		Message: message,
	}
}

func missingAppointmentTypeFacts(status, ageBand string) []string {
	var missing []string
	if status == "" {
		missing = append(missing, "patientStatus")
	}
	if ageBand == "" {
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

func appointmentAgeBand(dob string) string {
	if age, ok := domain.AgeYears(dob); ok {
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
