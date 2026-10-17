package scheduling

import (
	"fmt"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func TestAbitaAppointmentTypeMatrix(t *testing.T) {
	medical := [6]int{1006, 1004, 1004, 1007, 1005, 1005}
	routine := [6]int{1010, 4244, 4244, 3364, 4245, 4245}
	rows := []struct {
		office                           string
		medical, opticalMedical, routine [6]int
		postOp                           int
	}{
		{"spring_hill", medical, medical, [6]int{1010, 4244, 0, 3364, 4245, 0}, 1008},
		{"crystal_river", [6]int{6167, 6167, 6167, 6169, 6169, 6169}, [6]int{}, [6]int{}, 6168},
		{"hollywood", medical, medical, routine, 1008},
		{"sweetwater", medical, medical, routine, 1008},
		{"north_miami_beach_optical", [6]int{}, medical, routine, 1008},
	}
	routings := []domain.RoutingRule{domain.RoutingAll, domain.RoutingBachOnly, domain.RoutingBachLicht, domain.RoutingOpticalOnly, domain.RoutingNotAccepted}
	ages := []int{40, 10, 5}
	for _, row := range rows {
		office, ok := domain.LookupOfficeByID(row.office)
		if !ok {
			t.Fatalf("unknown office %s", row.office)
		}
		for _, routing := range routings {
			for _, category := range []string{"medical", "routine_vision"} {
				for statusIndex, status := range []string{"new", "established"} {
					for ageIndex, age := range ages {
						for _, reason := range []string{"eye exam", "recent surgery follow-up"} {
							want := row.medical[statusIndex*3+ageIndex]
							if routing == domain.RoutingNotAccepted {
								want = 0
							}
							if routing == domain.RoutingOpticalOnly {
								want = row.opticalMedical[statusIndex*3+ageIndex]
							}
							if category == "routine_vision" {
								want = row.routine[statusIndex*3+ageIndex]
							}
							if reason == "recent surgery follow-up" {
								want = row.postOp
								if routing == domain.RoutingOpticalOnly {
									want = 0
								}
							}
							name := fmt.Sprintf("%s/%s/%s/%s/age%d/%s", row.office, routing, category, status, age, reason)
							t.Run(name, func(t *testing.T) {
								got := resolveAppointmentTypeForIntent(office, routing, appointmentIntent{
									VisitCategory: category, PatientStatus: status, DOB: time.Now().AddDate(-age, 0, -1).Format("01/02/2006"), VisitReason: reason,
								})
								if got.AppointmentTypeID != want {
									t.Fatalf("type = %d, want %d; missing=%v", got.AppointmentTypeID, want, got.Missing)
								}
								if want == 0 && len(got.Missing) == 0 {
									t.Fatal("unresolved type needs a visible missing fact")
								}
							})
						}
					}
				}
			}
		}
	}
}
