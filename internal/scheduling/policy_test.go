package scheduling

import (
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

var policyTestNow = time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)

func TestAppointmentTypeMatchesDOBUsesCatalogAgeBand(t *testing.T) {
	pediatricDOB := policyTestNow.AddDate(-10, 0, 0).Format("01/02/2006")
	adultDOB := policyTestNow.AddDate(-40, 0, 0).Format("01/02/2006")
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
	policy := newSchedulingPolicy(domain.DefaultOffice(), policyTestNow)
	columnID := 1513
	profileID := 620
	_, policyErr := policy.PrepareBooking(bookingPolicyRequest{
		ColumnID: columnID, ProfileID: profileID, AppointmentTypeID: 9999, Routing: domain.RoutingAll,
	})
	const want = "Invalid appointment type ID: 9999. Valid types: 1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169"
	if policyErr == nil || policyErr.Message != want {
		t.Fatalf("PrepareBooking invalid-type error = %+v, want %q", policyErr, want)
	}
}

func TestSchedulingPolicy_PrepareBookingRejectsAppointmentTypeForWrongAge(t *testing.T) {
	policy := newSchedulingPolicy(domain.DefaultOffice(), policyTestNow)
	adultDOB := policyTestNow.AddDate(-30, 0, 0).Format("01/02/2006")

	_, policyErr := policy.PrepareBooking(bookingPolicyRequest{
		ColumnID:          1513,
		ProfileID:         620,
		AppointmentTypeID: 1004,
		Routing:           domain.RoutingBachOnly,
		DOB:               adultDOB,
	})
	if policyErr == nil {
		t.Fatal("adult booking with pediatric appointment type succeeded")
	}
}
