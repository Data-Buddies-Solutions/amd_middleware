package insurance

import (
	"slices"
	"strings"
	"testing"
)

func TestEveryPlanNameResolvesToItsPlan(t *testing.T) {
	for _, list := range catalog {
		for _, officeID := range list.Offices {
			o := office(t, officeID)
			for _, p := range list.Plans {
				if !officeSupports(o, p.Coverage) {
					continue
				}
				byID := DecidePlan(p.ID, p.Coverage, o, adultDOB)
				for _, name := range planNames(p) {
					if byName := DecideInsurance(name, p.Coverage, o, adultDOB); byName.PlanID != p.ID || byName.Outcome != byID.Outcome {
						t.Errorf("%s %q: by name %+v, by id %+v", officeID, name, byName, byID)
					}
				}
			}
		}
	}
}

func TestRealDataAetnaBetterAtSouthFloridaOffices(t *testing.T) {
	hollywood := DecideInsurance("Aetna Better", "medical", office(t, "hollywood"), adultDOB)
	sweetwater := DecideInsurance("Aetna Better", "medical", office(t, "sweetwater"), adultDOB)
	if hollywood.Outcome != "not_accepted" || hollywood.PlanID != sweetwater.PlanID || hollywood.PlanID == "" {
		t.Fatalf("hollywood = %+v", hollywood)
	}
	if sweetwater.Outcome != "accepted" || sweetwater.CarrierID != "car40907" || !slices.Equal(sweetwater.AllowedProviders, []string{"Dr. Bach"}) {
		t.Fatalf("sweetwater = %+v", sweetwater)
	}
}

func TestRealDataBareAetnaAsksWithResolvableOptions(t *testing.T) {
	for _, officeID := range []string{"hollywood", "sweetwater"} {
		o := office(t, officeID)
		d := DecideInsurance("Aetna", "medical", o, adultDOB)
		if d.Outcome != "needs_clarification" || len(d.Options) < 2 {
			t.Fatalf("%s: decision = %+v", officeID, d)
		}
		for _, option := range d.Options {
			if again := DecideInsurance(option.Label, "medical", o, adultDOB); again.PlanID != option.PlanID {
				t.Fatalf("%s: option %q resolved to %+v", officeID, option.Label, again)
			}
		}
	}
}

func TestRealDataSouthFloridaKnowsTheNamesMainAccepted(t *testing.T) {
	for heard, planID := range map[string]string{
		"Meritain Health - Aetna":          "meritain-health",
		"Tricare Humana Military (Select)": "tricare-select",
		"Tricare Humana Military (Prime)":  "tricare-prime",
	} {
		if d := DecideInsurance(heard, "medical", office(t, "hollywood"), adultDOB); d.PlanID != planID {
			t.Errorf("%q: decision = %+v", heard, d)
		}
	}
}

func TestRealDataSpringHillHumanaMedicareKeepsItsCarrier(t *testing.T) {
	d := DecideInsurance("Humana Medicare", "medical", office(t, "spring_hill"), adultDOB)
	if d.Outcome != "accepted" || d.CarrierID != "car40906" || !slices.Equal(d.AllowedProviders, []string{"Dr. Bach"}) {
		t.Fatalf("decision = %+v", d)
	}
}

func TestRealDataSimplyMedicaidAsksForTheSubscriberNumber(t *testing.T) {
	for _, c := range []struct{ office, coverage string }{
		{"hollywood", "medical"}, {"hollywood", "routine_vision"},
		{"sweetwater", "medical"}, {"sweetwater", "routine_vision"},
		{"north_miami_beach_optical", "routine_vision"},
		{"spring_hill", "medical"}, {"spring_hill", "routine_vision"},
	} {
		d := DecideInsurance("Simply Medicaid", c.coverage, office(t, c.office), adultDOB)
		if d.Outcome != "accepted" || d.CallerNotice != simplySubscriberNotice || !strings.HasSuffix(d.Answer, simplySubscriberNotice) {
			t.Errorf("%s %s: decision = %+v", c.office, c.coverage, d)
		}
	}
}

const simplySubscriberNotice = "For Simply Medicaid, we need the subscriber number on your card that starts with 7, not the Medicaid number that starts with 8."
