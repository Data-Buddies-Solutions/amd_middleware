package insurance

import (
	"slices"
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
	sweetwater := office(t, "sweetwater")
	d := DecideInsurance("Aetna", "medical", sweetwater, adultDOB)
	if d.Outcome != "needs_clarification" || len(d.Options) < 2 {
		t.Fatalf("decision = %+v", d)
	}
	for _, option := range d.Options {
		if again := DecideInsurance(option.Label, "medical", sweetwater, adultDOB); again.PlanID != option.PlanID {
			t.Fatalf("option %q resolved to %+v", option.Label, again)
		}
	}
	if d := DecideInsurance("Aetna", "medical", office(t, "hollywood"), adultDOB); d.Answer != answerAskFullName {
		t.Fatalf("hollywood has more than %d Aetna outcomes: %+v", maxOptions, d)
	}
}

func TestRealDataSpringHillHumanaMedicareKeepsItsCarrier(t *testing.T) {
	d := DecideInsurance("Humana Medicare", "medical", office(t, "spring_hill"), adultDOB)
	if d.Outcome != "accepted" || d.CarrierID != "car40906" || !slices.Equal(d.AllowedProviders, []string{"Dr. Bach"}) {
		t.Fatalf("decision = %+v", d)
	}
}
