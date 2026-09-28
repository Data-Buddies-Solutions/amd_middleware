package domain

const (
	AppointmentVisitMedical       = "medical"
	AppointmentVisitRoutineVision = "routine_vision"
	AppointmentVisitPostOp        = "post_op"

	AppointmentPatientNew         = "new"
	AppointmentPatientEstablished = "established"

	AppointmentAgeAdult     = "adult"
	AppointmentAgePediatric = "pediatric"
)

func AppointmentVisitType(typeID int) string {
	switch typeID {
	case 1004, 1005, 1006, 1007, 1008, 6167, 6168, 6169:
		return AppointmentVisitMedical
	case 1010, 3364, 4244, 4245:
		return AppointmentVisitRoutineVision
	default:
		return ""
	}
}
