package domain

import (
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
