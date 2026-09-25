package domain

import (
	"testing"
)

func TestLegacyAuthorizationIsOfficeScoped(t *testing.T) {
	for _, name := range []string{"Hollywood", "Sweetwater", "Spring Hill", "Crystal River"} {
		office, _ := ResolveOffice(name)
		d := DecideInsurance("Aetna HMO", "medical", office, "01/02/1980")
		needsAuth := name == "Hollywood" || name == "Sweetwater"
		if needsAuth && (d.Outcome != "needs_staff_task" || d.CanSchedule || len(d.Requirements) == 0) {
			t.Fatalf("lost old authorization at %s: %+v", name, d)
		}
		if !needsAuth && len(d.Requirements) > 0 {
			t.Fatalf("added authorization at %s: %+v", name, d)
		}
	}
}
