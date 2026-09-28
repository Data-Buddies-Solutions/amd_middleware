package scheduling

import (
	"advancedmd-token-management/internal/domain"
	"testing"
)

func TestResolveAppointmentTypeForIntent(t *testing.T) {
	springHill := domain.DefaultOffice()
	crystalRiver, ok := domain.LookupOffice("Crystal River")
	if !ok {
		t.Fatal("Crystal River office not found")
	}
	northMiamiBeachOptical, ok := domain.LookupOffice("North Miami Beach Optical")
	if !ok {
		t.Fatal("North Miami Beach Optical office not found")
	}

	tests := []struct {
		name    string
		office  *domain.OfficeConfig
		routing domain.RoutingRule
		intent  appointmentIntent
		wantID  int
		missing []string
	}{
		{
			name:    "new adult medical at Spring Hill",
			office:  springHill,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientNew,
				DOB:           "01/01/1980",
			},
			wantID: 1006,
		},
		{
			name:    "new pediatric medical at Spring Hill",
			office:  springHill,
			routing: domain.RoutingBachOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientNew,
				DOB:           "01/01/2015",
			},
			wantID: 1004,
		},
		{
			name:    "established adult medical at Spring Hill",
			office:  springHill,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgeAdult,
			},
			wantID: 1007,
		},
		{
			name:    "established pediatric medical at Spring Hill",
			office:  springHill,
			routing: domain.RoutingBachOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgePediatric,
			},
			wantID: 1005,
		},
		{
			name:    "post-op at Spring Hill",
			office:  springHill,
			routing: domain.RoutingBachOnly,
			intent: appointmentIntent{
				VisitKind:     domain.AppointmentVisitPostOp,
				PatientStatus: domain.AppointmentPatientEstablished,
				DOB:           "01/01/1980",
			},
			wantID: 1008,
		},
		{
			name:    "post-op from visit reason",
			office:  springHill,
			routing: domain.RoutingBachOnly,
			intent: appointmentIntent{
				VisitReason:   "recent surgery follow-up",
				PatientStatus: domain.AppointmentPatientEstablished,
				DOB:           "01/01/1980",
			},
			wantID: 1008,
		},
		{
			name:    "new adult routine vision",
			office:  springHill,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitRoutineVision,
				PatientStatus: domain.AppointmentPatientNew,
				AgeBand:       domain.AppointmentAgeAdult,
			},
			wantID: 1010,
		},
		{
			name:    "established adult routine vision",
			office:  springHill,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitRoutineVision,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgeAdult,
			},
			wantID: 3364,
		},
		{
			name:    "new pediatric routine vision",
			office:  springHill,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitRoutineVision,
				PatientStatus: domain.AppointmentPatientNew,
				AgeBand:       domain.AppointmentAgePediatric,
				DOB:           "01/01/2018",
			},
			wantID: 4244,
		},
		{
			name:    "established pediatric routine vision",
			office:  springHill,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitRoutineVision,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgePediatric,
				DOB:           "01/01/2018",
			},
			wantID: 4245,
		},
		{
			name:    "Crystal River new patient",
			office:  crystalRiver,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientNew,
			},
			wantID: 6167,
		},
		{
			name:    "Crystal River established patient",
			office:  crystalRiver,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientEstablished,
			},
			wantID: 6169,
		},
		{
			name:    "Crystal River post-op",
			office:  crystalRiver,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitKind:     domain.AppointmentVisitPostOp,
				PatientStatus: domain.AppointmentPatientEstablished,
			},
			wantID: 6168,
		},
		{
			name:    "routine vision defaults from optical routing",
			office:  springHill,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				PatientStatus: domain.AppointmentPatientEstablished,
				DOB:           "01/01/1980",
			},
			wantID: 3364,
		},
		{
			name:    "routine vision at North Miami Beach Optical",
			office:  northMiamiBeachOptical,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitRoutineVision,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgeAdult,
			},
			wantID: 3364,
		},
		{
			name:    "requires status and DOB for non-Crystal River medical",
			office:  springHill,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
			},
			missing: []string{"patientStatus", "dob"},
		},
		{
			name:    "North Miami Beach Optical has no medical lane",
			office:  northMiamiBeachOptical,
			routing: domain.RoutingAll,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitMedical,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgeAdult,
			},
			missing: []string{"routing"},
		},
		{
			name:    "requires routing to Spring Hill for Crystal River routine vision",
			office:  crystalRiver,
			routing: domain.RoutingOpticalOnly,
			intent: appointmentIntent{
				VisitCategory: domain.AppointmentVisitRoutineVision,
				PatientStatus: domain.AppointmentPatientEstablished,
				AgeBand:       domain.AppointmentAgeAdult,
			},
			missing: []string{"routeToSpringHill"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAppointmentTypeForIntent(tt.office, tt.routing, tt.intent)
			if got.AppointmentTypeID != tt.wantID {
				t.Fatalf("AppointmentTypeID = %d, want %d (missing=%v message=%q)", got.AppointmentTypeID, tt.wantID, got.Missing, got.Message)
			}
			if !sameStringSlice(got.Missing, tt.missing) {
				t.Fatalf("Missing = %v, want %v", got.Missing, tt.missing)
			}
			if tt.wantID != 0 && got.AppointmentTypeName == "" {
				t.Fatal("AppointmentTypeName should be set for resolved types")
			}
		})
	}
}

func TestResolveAppointmentTypeForIntent_SpringHillUnderSevenRoutineVision(t *testing.T) {
	got := resolveAppointmentTypeForIntent(domain.DefaultOffice(), domain.RoutingOpticalOnly, appointmentIntent{
		VisitCategory: domain.AppointmentVisitRoutineVision,
		PatientStatus: domain.AppointmentPatientNew,
		AgeBand:       domain.AppointmentAgePediatric,
		DOB:           "01/01/2021",
	})

	if got.AppointmentTypeID != 0 {
		t.Fatalf("AppointmentTypeID = %d, want unresolved", got.AppointmentTypeID)
	}
	if got.Message != "Spring Hill does not schedule routine vision for children under 7. Treat the visit as medical and schedule with Dr. Bach on the Spring Hill medical lane." {
		t.Fatalf("Message = %q", got.Message)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "appointmentLane" {
		t.Fatalf("Missing = %v, want appointmentLane", got.Missing)
	}
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
