package scheduling

import (
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

var policyTestNow = time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)

func TestAppointmentTypeMatchesDOBUsesCatalogAgeBand(t *testing.T) {
	pediatricDOB := domain.DOB(policyTestNow.AddDate(-10, 0, 0).Format("01/02/2006"))
	adultDOB := domain.DOB(policyTestNow.AddDate(-40, 0, 0).Format("01/02/2006"))
	for id, appointmentType := range domain.AppointmentTypes {
		wantPediatric := appointmentType.AgeBand != domain.AppointmentAgeAdult
		wantAdult := appointmentType.AgeBand != domain.AppointmentAgePediatric
		if got := appointmentTypeMatchesDOB(id, pediatricDOB, policyTestNow); got != wantPediatric {
			t.Errorf("appointmentTypeMatchesDOB(%d, pediatric DOB, policyTestNow) = %v, want %v", id, got, wantPediatric)
		}
		if got := appointmentTypeMatchesDOB(id, adultDOB, policyTestNow); got != wantAdult {
			t.Errorf("appointmentTypeMatchesDOB(%d, adult DOB, policyTestNow) = %v, want %v", id, got, wantAdult)
		}
	}
	if !appointmentTypeMatchesDOB(9999, "01/01/1980", policyTestNow) {
		t.Error("unknown appointment type should retain age-agnostic behavior")
	}
}

func TestInvalidAppointmentTypeMessageUsesCatalog(t *testing.T) {
	policy := newSchedulingPolicy(officeResolvingOnly(domain.DefaultOffice(), 9999), policyTestNow)
	columnID := 1513
	profileID := 620
	_, policyErr := policy.PrepareBooking(bookingPolicyRequest{
		ColumnID: columnID, ProfileID: profileID, Routing: domain.RoutingAll,
	})
	const want = "Invalid appointment type ID: 9999. Valid types: 1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169"
	if policyErr == nil || policyErr.Message != want {
		t.Fatalf("PrepareBooking invalid-type error = %+v, want %q", policyErr, want)
	}
}

func TestSchedulingPolicy_FarnanAdultVisionColumnBooksOnlyAdultVision(t *testing.T) {
	office, ok := domain.LookupOfficeByID("hollywood")
	if !ok {
		t.Fatal("expected Hollywood office")
	}
	policy := newSchedulingPolicy(office, policyTestNow)
	adult := domain.DOB(policyTestNow.AddDate(-18, 0, 0).Format("01/02/2006"))
	minor := domain.DOB(policyTestNow.AddDate(-18, 0, 1).Format("01/02/2006"))
	tests := []struct {
		name    string
		typeID  int
		routing domain.RoutingRule
		dob     domain.DOB
		wantErr string
	}{
		{name: "new adult vision", typeID: 1010, routing: domain.RoutingOpticalOnly, dob: adult},
		{name: "established adult vision", typeID: 3364, routing: domain.RoutingOpticalOnly, dob: adult},
		{name: "minor", typeID: 4245, routing: domain.RoutingOpticalOnly, dob: minor, wantErr: "Dr. Farnan requires patient age 18 or older"},
		{name: "no DOB", typeID: 1010, routing: domain.RoutingOpticalOnly, wantErr: "Dr. Farnan requires patient DOB to verify age 18 or older"},
		{name: "medical", typeID: 1007, routing: domain.RoutingAll, dob: adult, wantErr: `Column 1615 is not valid for routing "all_three" at Hollywood`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := newSchedulingPolicy(officeResolvingOnly(policy.office, tt.typeID), policy.asOf)
			_, policyErr := policy.PrepareBooking(bookingPolicyRequest{
				ColumnID: 1615, ProfileID: 2075, Routing: tt.routing, DOB: tt.dob,
				Intent: appointmentIntent{VisitCategory: domain.AppointmentVisitType(tt.typeID)},
			})
			got := ""
			if policyErr != nil {
				got = policyErr.Message
			}
			if got != tt.wantErr {
				t.Fatalf("PrepareBooking error = %q, want %q", got, tt.wantErr)
			}
		})
	}

	columns := []domain.SchedulerColumn{{ID: 1555, FacilityID: 1480}, {ID: 1615, FacilityID: 1480}}
	if got := policy.EligibleColumns(columns, domain.RoutingOpticalOnly, minor); len(got) != 1 || got[0].ID != 1555 {
		t.Fatalf("EligibleColumns for a 17-year-old = %+v, want only 1555", got)
	}
}

func TestSchedulingPolicy_PrepareBookingRejectsAppointmentTypeForWrongAge(t *testing.T) {
	policy := newSchedulingPolicy(domain.DefaultOffice(), policyTestNow)
	adultDOB := domain.DOB(policyTestNow.AddDate(-30, 0, 0).Format("01/02/2006"))

	policy = newSchedulingPolicy(officeResolvingOnly(policy.office, 1004), policy.asOf)
	_, policyErr := policy.PrepareBooking(bookingPolicyRequest{
		ColumnID:  1513,
		ProfileID: 620,
		Routing:   domain.RoutingBachOnly,
		DOB:       adultDOB,
		Intent:    appointmentIntent{VisitCategory: domain.AppointmentVisitMedical},
	})
	if policyErr == nil {
		t.Fatal("adult booking with pediatric appointment type succeeded")
	}
}

func officeResolvingOnly(office *domain.OfficeConfig, typeID int) *domain.OfficeConfig {
	resolving := *office
	ladder := domain.AppointmentTypeLadder{NewPediatric: typeID, NewAdult: typeID, EstablishedPediatric: typeID, EstablishedAdult: typeID}
	resolving.AppointmentTypes = domain.OfficeAppointmentTypes{PostOp: typeID, Medical: ladder, RoutineVision: ladder}
	return &resolving
}
