package scheduling

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
)

var updateAppointmentTypeGolden = flag.Bool("update", false, "rewrite the golden appointment type files in testdata")

var goldenOffices = []string{"spring_hill", "crystal_river", "hollywood", "sweetwater", "north_miami_beach_optical"}

var goldenRoutingRules = []domain.RoutingRule{domain.RoutingNotAccepted, domain.RoutingBachOnly, domain.RoutingBachLicht, domain.RoutingAll, domain.RoutingOpticalOnly}

var goldenBookingTypes = []int{0, 1004, 1005, 1006, 1007, 1008, 1010, 3364, 4244, 4245, 6167, 6168, 6169, 9999}

func goldenAgeDOB(age int) string {
	return policyTestNow.AddDate(-age, 0, -1).Format("01/02/2006")
}

func TestAppointmentTypeDecisionsMatchGolden(t *testing.T) {
	for _, env := range []string{"prod", "dev"} {
		t.Run(env, func(t *testing.T) {
			t.Cleanup(func() { domain.InitRegistry("prod") })
			domain.InitRegistry(env)
			checkAppointmentTypeGolden(t, filepath.Join("testdata", "appointment_types."+env+".golden"), appointmentTypeRows())
		})
	}
}

func checkAppointmentTypeGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateAppointmentTypeGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/scheduling -run Golden -update)", err)
	}
	if got == string(want) {
		return
	}
	wantRows := strings.Split(string(want), "\n")
	gotRows := strings.Split(got, "\n")
	diffs := 0
	for i := range max(len(wantRows), len(gotRows)) {
		w, g := "", ""
		if i < len(wantRows) {
			w = wantRows[i]
		}
		if i < len(gotRows) {
			g = gotRows[i]
		}
		if w != g {
			t.Errorf("%s row %d\nwant %s\n got %s", filepath.Base(path), i+1, w, g)
			if diffs++; diffs == 20 {
				break
			}
		}
	}
	t.Fatalf("appointment type decisions changed; if intended, run go test ./internal/scheduling -run Golden -update and review the diff of %s", filepath.Base(path))
}

func appointmentTypeRows() string {
	var b strings.Builder
	dobs := []struct {
		label string
		dob   string
	}{{"none", ""}, {"age6", goldenAgeDOB(6)}, {"age7", goldenAgeDOB(7)}, {"age10", goldenAgeDOB(10)}, {"age40", goldenAgeDOB(40)}}
	for _, id := range goldenOffices {
		office, ok := domain.LookupOfficeByID(id)
		if !ok {
			fmt.Fprintf(&b, "office\t%s\tnot found\n", id)
			continue
		}
		policy := newSchedulingPolicy(office, policyTestNow)
		for age := 3; age <= 8; age++ {
			fmt.Fprintf(&b, "tooYoung\t%s\tage%d\t%t\n", id, age, routineVisionTooYoung(office, age))
		}
		for _, routing := range goldenRoutingRules {
			for _, d := range dobs {
				fmt.Fprintf(&b, "allowed\t%s\t%s\t%s\t%v\n", id, routing, d.label, policy.AllowedAppointmentTypeIDs(routing, d.dob))
			}
			for _, category := range []string{domain.AppointmentVisitMedical, domain.AppointmentVisitRoutineVision} {
				for _, status := range []string{"", domain.AppointmentPatientNew, domain.AppointmentPatientEstablished} {
					for _, d := range dobs {
						for _, reason := range []string{"eye exam", "post-op check"} {
							got := resolveAppointmentTypeForIntent(office, routing, appointmentIntent{
								VisitCategory: category, PatientStatus: status, DOB: d.dob, VisitReason: reason,
							}, policyTestNow)
							fmt.Fprintf(&b, "intent\t%s\t%s\t%s\t%s\t%s\t%s\ttype=%d\tmissing=%s\tmessage=%s\n",
								id, routing, category, status, d.label, reason, got.AppointmentTypeID, strings.Join(got.Missing, ","), got.Message)
						}
					}
				}
			}
		}
		columnIDs := make([]string, 0, len(office.Columns))
		for columnID := range office.Columns {
			columnIDs = append(columnIDs, columnID)
		}
		slices.Sort(columnIDs)
		for _, columnID := range columnIDs {
			column := office.Columns[columnID]
			columnNumber, _ := strconv.Atoi(columnID)
			profileNumber, _ := strconv.Atoi(column.ProfileID)
			for _, routing := range goldenRoutingRules {
				if !office.ColumnsForRouting(routing)[columnID] {
					continue
				}
				for _, typeID := range goldenBookingTypes {
					category := domain.AppointmentVisitMedical
					if routing == domain.RoutingOpticalOnly {
						category = domain.AppointmentVisitRoutineVision
					}
					decision, policyErr := policy.PrepareBooking(bookingPolicyRequest{
						ColumnID: columnNumber, ProfileID: profileNumber, AppointmentTypeID: typeID, Routing: routing, DOB: goldenAgeDOB(40),
						Intent: appointmentIntent{VisitCategory: category, PatientStatus: domain.AppointmentPatientNew, DOB: goldenAgeDOB(40)},
					})
					if policyErr != nil {
						fmt.Fprintf(&b, "prepare\t%s\t%s\t%s\t%d\terror\t%s\t%s\n", id, columnID, routing, typeID, policyErr.Outcome, policyErr.Message)
						continue
					}
					fmt.Fprintf(&b, "prepare\t%s\t%s\t%s\t%d\trouting=%s\ttype=%d\tprovider=%d\tcolor=%s\n",
						id, columnID, routing, typeID, decision.Routing, decision.AppointmentTypeID, decision.EnvironmentTypeID, decision.Color)
				}
			}
			decision, policyErr := policy.PrepareBooking(bookingPolicyRequest{
				ColumnID: columnNumber, ProfileID: profileNumber, Routing: domain.RoutingAll, DOB: goldenAgeDOB(40), PreservedAppointmentTypeID: 9999,
			})
			fmt.Fprintf(&b, "preserve\t%s\t%s\t%+v\t%+v\n", id, columnID, decision, policyErr)
		}
	}
	return b.String()
}
