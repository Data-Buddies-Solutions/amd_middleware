package domain

import (
	"slices"
	"testing"
	"time"
)

func TestSchedulingPolicy_PreservesPatientAndSchedulingPediatricRules(t *testing.T) {
	policy := NewSchedulingPolicy(DefaultOffice())
	minorDOB := time.Now().AddDate(-10, 0, 0).Format("01/02/2006")

	if got := policy.SchedulingRouting(RoutingOpticalOnly, minorDOB); got != RoutingOpticalOnly {
		t.Fatalf("scheduling routing = %q, want optical_only", got)
	}
	if got := policy.PatientRouting(RoutingOpticalOnly, minorDOB); got != RoutingBachOnly {
		t.Fatalf("patient routing = %q, want bach_only", got)
	}
}

func TestSchedulingPolicy_AllowedAppointmentTypeIDsFiltersByDOB(t *testing.T) {
	policy := NewSchedulingPolicy(DefaultOffice())
	adultDOB := time.Now().AddDate(-30, 0, 0).Format("01/02/2006")
	minorDOB := time.Now().AddDate(-10, 0, 0).Format("01/02/2006")

	if got, want := policy.AllowedAppointmentTypeIDs(RoutingBachOnly, adultDOB), []int{1006, 1007, 1008}; !slices.Equal(got, want) {
		t.Fatalf("adult appointment types = %v, want %v", got, want)
	}
	if got, want := policy.AllowedAppointmentTypeIDs(RoutingBachOnly, minorDOB), []int{1004, 1005, 1008}; !slices.Equal(got, want) {
		t.Fatalf("pediatric appointment types = %v, want %v", got, want)
	}
}

func TestSchedulingPolicy_PrepareBookingRejectsAppointmentTypeForWrongAge(t *testing.T) {
	policy := NewSchedulingPolicy(DefaultOffice())
	adultDOB := time.Now().AddDate(-30, 0, 0).Format("01/02/2006")

	_, policyErr := policy.PrepareBooking(BookingPolicyRequest{
		ColumnID:          1513,
		ProfileID:         620,
		AppointmentTypeID: 1004,
		Routing:           RoutingBachOnly,
		DOB:               adultDOB,
	})
	if policyErr == nil {
		t.Fatal("adult booking with pediatric appointment type succeeded")
	}
}
