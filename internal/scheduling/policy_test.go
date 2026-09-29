package scheduling

import (
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func TestSchedulingPolicy_PrepareBookingRejectsAppointmentTypeForWrongAge(t *testing.T) {
	policy := newSchedulingPolicy(domain.DefaultOffice())
	adultDOB := time.Now().AddDate(-30, 0, 0).Format("01/02/2006")

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
