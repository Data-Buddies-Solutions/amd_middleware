package scheduling

import (
	"fmt"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func TestAppointmentTypeMatchesDOBUsesCatalogAgeBand(t *testing.T) {
	for id, appointmentType := range domain.AppointmentTypes {
		wantPediatric := appointmentType.AgeBand != domain.AppointmentAgeAdult
		wantAdult := appointmentType.AgeBand != domain.AppointmentAgePediatric
		if got := appointmentTypeMatchesDOB(id, "01/01/2015"); got != wantPediatric {
			t.Errorf("appointmentTypeMatchesDOB(%d, pediatric DOB) = %v, want %v", id, got, wantPediatric)
		}
		if got := appointmentTypeMatchesDOB(id, "01/01/1980"); got != wantAdult {
			t.Errorf("appointmentTypeMatchesDOB(%d, adult DOB) = %v, want %v", id, got, wantAdult)
		}
	}
	if !appointmentTypeMatchesDOB(9999, "01/01/1980") {
		t.Error("unknown appointment type should retain age-agnostic behavior")
	}
}

func TestInvalidAppointmentTypeMessageUsesCatalog(t *testing.T) {
	policy := newSchedulingPolicy(domain.DefaultOffice())
	columnID := 1513
	profileID := 620
	_, policyErr := policy.PrepareBooking(bookingPolicyRequest{
		ColumnID: columnID, ProfileID: profileID, AppointmentTypeID: 9999, Routing: domain.RoutingAll,
	})
	want := fmt.Sprintf("Invalid appointment type ID: 9999. Valid types: %s", domain.ValidAppointmentTypeIDs())
	if policyErr == nil || policyErr.Message != want {
		t.Fatalf("PrepareBooking invalid-type error = %+v, want %q", policyErr, want)
	}
}

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
