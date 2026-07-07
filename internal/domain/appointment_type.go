package domain

import (
	"fmt"
	"strings"
)

const (
	AppointmentVisitMedical       = "medical"
	AppointmentVisitRoutineVision = "routine_vision"
	AppointmentVisitPostOp        = "post_op"

	AppointmentPatientNew         = "new"
	AppointmentPatientEstablished = "established"

	AppointmentAgeAdult     = "adult"
	AppointmentAgePediatric = "pediatric"
)

// AppointmentIntent contains the model-facing booking intent. Numeric AMD
// appointment type IDs are resolved server-side from these stable facts.
type AppointmentIntent struct {
	VisitCategory string
	VisitKind     string
	PatientStatus string
	AgeBand       string
	DOB           string
	IsPostOp      bool
	VisitReason   string
}

type AppointmentTypeResolution struct {
	AppointmentTypeID   int
	AppointmentTypeName string
	Missing             []string
	Message             string
}

func ResolveAppointmentTypeForIntent(office *OfficeConfig, routing RoutingRule, intent AppointmentIntent) AppointmentTypeResolution {
	if office == nil {
		return unresolvedAppointmentType([]string{"office"}, "Office is required to resolve appointment type.")
	}

	visitKind := normalizeAppointmentVisitKind(intent.VisitKind)
	postOp := intent.IsPostOp || visitKind == AppointmentVisitPostOp || appointmentReasonLooksPostOp(intent.VisitReason)
	category := normalizeAppointmentVisitCategory(intent.VisitCategory, visitKind, routing)
	status := normalizeAppointmentPatientStatus(intent.PatientStatus)
	ageBand := normalizeAppointmentAgeBand(intent.AgeBand, intent.DOB)

	if postOp {
		if routing == RoutingOpticalOnly {
			return unresolvedAppointmentType([]string{"routing"}, "Post-op appointments must use a medical scheduling lane.")
		}
		if office.ID == "crystal_river" {
			return resolvedAppointmentType(6168)
		}
		return resolvedAppointmentType(1008)
	}

	if category == AppointmentVisitRoutineVision {
		if len(office.ColumnsForRouting(RoutingOpticalOnly)) == 0 {
			return unresolvedAppointmentType([]string{"routeToSpringHill"}, fmt.Sprintf("Routine vision scheduling is not supported at %s. Route the visit to Spring Hill before booking.", office.DisplayName))
		}

		missing := missingAppointmentTypeFacts(status, ageBand)
		if len(missing) > 0 {
			return unresolvedAppointmentType(missing, appointmentTypeMissingFactsMessage(missing))
		}
		if office.ID == "spring_hill" && ageBand == AppointmentAgePediatric {
			age, ok := AgeYears(intent.DOB)
			if !ok {
				return unresolvedAppointmentType([]string{"dob"}, "Patient DOB is required before scheduling pediatric routine vision at Spring Hill.")
			}
			if age < 7 {
				return unresolvedAppointmentType([]string{"appointmentLane"}, "Spring Hill does not schedule routine vision for children under 7. Treat the visit as medical and schedule with Dr. Bach on the Spring Hill medical lane.")
			}
		}

		if status == AppointmentPatientNew {
			if ageBand == AppointmentAgePediatric {
				return resolvedAppointmentType(4244)
			}
			return resolvedAppointmentType(1010)
		}
		if ageBand == AppointmentAgePediatric {
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
		if status == AppointmentPatientNew {
			return resolvedAppointmentType(6167)
		}
		return resolvedAppointmentType(6169)
	}

	missing := missingAppointmentTypeFacts(status, ageBand)
	if len(missing) > 0 {
		return unresolvedAppointmentType(missing, appointmentTypeMissingFactsMessage(missing))
	}

	if status == AppointmentPatientNew {
		if ageBand == AppointmentAgePediatric {
			return resolvedAppointmentType(1004)
		}
		return resolvedAppointmentType(1006)
	}
	if ageBand == AppointmentAgePediatric {
		return resolvedAppointmentType(1005)
	}
	return resolvedAppointmentType(1007)
}

func resolvedAppointmentType(typeID int) AppointmentTypeResolution {
	return AppointmentTypeResolution{
		AppointmentTypeID:   typeID,
		AppointmentTypeName: DefaultAppointmentTypeNames[typeID],
	}
}

func unresolvedAppointmentType(missing []string, message string) AppointmentTypeResolution {
	return AppointmentTypeResolution{
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

func normalizeAppointmentVisitCategory(category, visitKind string, routing RoutingRule) string {
	kind := normalizeAppointmentVisitKind(visitKind)
	if kind == AppointmentVisitRoutineVision {
		return AppointmentVisitRoutineVision
	}

	switch normalizeAppointmentToken(category) {
	case "routine vision", "routine eye exam", "vision", "optical", "optical only":
		return AppointmentVisitRoutineVision
	case "medical", "medical visit", "follow up", "followup":
		return AppointmentVisitMedical
	}

	if routing == RoutingOpticalOnly {
		return AppointmentVisitRoutineVision
	}
	return AppointmentVisitMedical
}

func normalizeAppointmentVisitKind(kind string) string {
	switch normalizeAppointmentToken(kind) {
	case "post op", "postop", "post operative", "postoperative":
		return AppointmentVisitPostOp
	case "routine vision", "routine eye exam", "vision", "optical", "optical only":
		return AppointmentVisitRoutineVision
	case "medical", "medical visit", "follow up", "followup":
		return AppointmentVisitMedical
	default:
		return ""
	}
}

func normalizeAppointmentPatientStatus(status string) string {
	switch normalizeAppointmentToken(status) {
	case "new", "created", "new patient":
		return AppointmentPatientNew
	case "established", "existing", "current", "current patient", "matched", "verified":
		return AppointmentPatientEstablished
	default:
		return ""
	}
}

func normalizeAppointmentAgeBand(ageBand, dob string) string {
	switch normalizeAppointmentToken(ageBand) {
	case "adult":
		return AppointmentAgeAdult
	case "pediatric", "paediatric", "minor", "child":
		return AppointmentAgePediatric
	}

	if age, ok := AgeYears(dob); ok {
		if age < 18 {
			return AppointmentAgePediatric
		}
		return AppointmentAgeAdult
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
