package domain

import "testing"

func TestAppointmentTypeCatalog(t *testing.T) {
	tests := []struct {
		id        int
		name      string
		color     string
		visitType string
		ageBand   string
	}{
		{1004, "New Pediatric Medical", "GREEN", AppointmentVisitMedical, AppointmentAgePediatric},
		{1005, "Established Pediatric Medical (Follow Up)", "PINK", AppointmentVisitMedical, AppointmentAgePediatric},
		{1006, "New Adult Medical", "RED", AppointmentVisitMedical, AppointmentAgeAdult},
		{1007, "Established Adult Medical (Follow Up)", "ORANGE", AppointmentVisitMedical, AppointmentAgeAdult},
		{1008, "Post Op", "BLUE", AppointmentVisitMedical, appointmentAgeAny},
		{1010, "New Adult Vision", "TEAL", AppointmentVisitRoutineVision, AppointmentAgeAdult},
		{3364, "Established Adult Vision", "ROSE", AppointmentVisitRoutineVision, AppointmentAgeAdult},
		{4244, "New Pediatric Vision", "BROWN", AppointmentVisitRoutineVision, AppointmentAgePediatric},
		{4245, "Established Pediatric Vision", "GRAY", AppointmentVisitRoutineVision, AppointmentAgePediatric},
		{6167, "Crystal River New Patient", "ORANGE", AppointmentVisitMedical, appointmentAgeAny},
		{6168, "Crystal River Post Op", "TEAL", AppointmentVisitMedical, appointmentAgeAny},
		{6169, "Crystal River Established Patient", "RED", AppointmentVisitMedical, appointmentAgeAny},
	}

	if len(AppointmentTypes) != len(tests) {
		t.Fatalf("AppointmentTypes has %d entries, want %d", len(AppointmentTypes), len(tests))
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := AppointmentTypes[test.id]
			if !ok {
				t.Fatalf("AppointmentTypes[%d] is missing", test.id)
			}
			if got.Name != test.name || got.Color != test.color || got.VisitType != test.visitType || got.AgeBand != test.ageBand {
				t.Errorf("AppointmentTypes[%d] = %+v", test.id, got)
			}
			if AppointmentVisitType(test.id) != test.visitType {
				t.Errorf("AppointmentVisitType(%d) = %q, want %q", test.id, AppointmentVisitType(test.id), test.visitType)
			}
		})
	}

	const valid = "1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169"
	if got := ValidAppointmentTypeIDs(); got != valid {
		t.Errorf("ValidAppointmentTypeIDs() = %q, want %q", got, valid)
	}
}
