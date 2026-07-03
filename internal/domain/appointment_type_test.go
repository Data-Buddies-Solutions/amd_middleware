package domain

import "testing"

func TestResolveAppointmentTypeForIntent(t *testing.T) {
	springHill := DefaultOffice()
	crystalRiver, ok := LookupOffice("Crystal River")
	if !ok {
		t.Fatal("Crystal River office not found")
	}
	northMiamiBeachOptical, ok := LookupOffice("North Miami Beach Optical")
	if !ok {
		t.Fatal("North Miami Beach Optical office not found")
	}

	tests := []struct {
		name    string
		office  *OfficeConfig
		routing RoutingRule
		intent  AppointmentIntent
		wantID  int
		missing []string
	}{
		{
			name:    "new adult medical at Spring Hill",
			office:  springHill,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientNew,
				DOB:           "01/01/1980",
			},
			wantID: 1006,
		},
		{
			name:    "new pediatric medical at Spring Hill",
			office:  springHill,
			routing: RoutingBachOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientNew,
				DOB:           "01/01/2015",
			},
			wantID: 1004,
		},
		{
			name:    "established adult medical at Spring Hill",
			office:  springHill,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgeAdult,
			},
			wantID: 1007,
		},
		{
			name:    "established pediatric medical at Spring Hill",
			office:  springHill,
			routing: RoutingBachOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgePediatric,
			},
			wantID: 1005,
		},
		{
			name:    "post-op at Spring Hill",
			office:  springHill,
			routing: RoutingBachOnly,
			intent: AppointmentIntent{
				VisitKind:     AppointmentVisitPostOp,
				PatientStatus: AppointmentPatientEstablished,
				DOB:           "01/01/1980",
			},
			wantID: 1008,
		},
		{
			name:    "post-op from visit reason",
			office:  springHill,
			routing: RoutingBachOnly,
			intent: AppointmentIntent{
				VisitReason:   "recent surgery follow-up",
				PatientStatus: AppointmentPatientEstablished,
				DOB:           "01/01/1980",
			},
			wantID: 1008,
		},
		{
			name:    "new adult routine vision",
			office:  springHill,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitRoutineVision,
				PatientStatus: AppointmentPatientNew,
				AgeBand:       AppointmentAgeAdult,
			},
			wantID: 1010,
		},
		{
			name:    "established adult routine vision",
			office:  springHill,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitRoutineVision,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgeAdult,
			},
			wantID: 3364,
		},
		{
			name:    "new pediatric routine vision",
			office:  springHill,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitRoutineVision,
				PatientStatus: AppointmentPatientNew,
				AgeBand:       AppointmentAgePediatric,
			},
			wantID: 4244,
		},
		{
			name:    "established pediatric routine vision",
			office:  springHill,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitRoutineVision,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgePediatric,
			},
			wantID: 4245,
		},
		{
			name:    "Crystal River new patient",
			office:  crystalRiver,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientNew,
			},
			wantID: 6167,
		},
		{
			name:    "Crystal River established patient",
			office:  crystalRiver,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientEstablished,
			},
			wantID: 6169,
		},
		{
			name:    "Crystal River post-op",
			office:  crystalRiver,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitKind:     AppointmentVisitPostOp,
				PatientStatus: AppointmentPatientEstablished,
			},
			wantID: 6168,
		},
		{
			name:    "routine vision defaults from optical routing",
			office:  springHill,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				PatientStatus: AppointmentPatientEstablished,
				DOB:           "01/01/1980",
			},
			wantID: 3364,
		},
		{
			name:    "routine vision at North Miami Beach Optical",
			office:  northMiamiBeachOptical,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitRoutineVision,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgeAdult,
			},
			wantID: 3364,
		},
		{
			name:    "requires status and DOB for non-Crystal River medical",
			office:  springHill,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
			},
			missing: []string{"patientStatus", "dob"},
		},
		{
			name:    "North Miami Beach Optical has no medical lane",
			office:  northMiamiBeachOptical,
			routing: RoutingAll,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitMedical,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgeAdult,
			},
			missing: []string{"routing"},
		},
		{
			name:    "requires routing to Spring Hill for Crystal River routine vision",
			office:  crystalRiver,
			routing: RoutingOpticalOnly,
			intent: AppointmentIntent{
				VisitCategory: AppointmentVisitRoutineVision,
				PatientStatus: AppointmentPatientEstablished,
				AgeBand:       AppointmentAgeAdult,
			},
			missing: []string{"routeToSpringHill"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAppointmentTypeForIntent(tt.office, tt.routing, tt.intent)
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
