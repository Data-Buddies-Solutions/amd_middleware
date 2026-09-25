package domain

import (
	"testing"
	"time"
)

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
