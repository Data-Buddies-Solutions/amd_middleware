package insurance

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestRealDataEmbossSpecialtyKeepsPremierCarrier(t *testing.T) {
	for _, officeID := range []string{"hollywood", "sweetwater", "north_miami_beach_optical", "spring_hill"} {
		t.Run(officeID, func(t *testing.T) {
			o := office(t, officeID)
			want := DecidePlan("premier-eye-care", "routine_vision", o, adultDOB, testNow)
			if want.Outcome != "accepted" || want.CanonicalPlan != "Emboss Specialty" || want.CarrierCode != "PREM1" || want.CarrierID != "car281317" {
				t.Errorf("renamed plan = %+v", want)
			}
			for _, name := range []string{"Emboss Specialty", "Premier Eye Care", "Premier"} {
				if got := DecideInsurance(name, "routine_vision", o, adultDOB, testNow); !reflect.DeepEqual(got, want) {
					t.Errorf("%q: got %+v, want %+v", name, got, want)
				}
			}
			summaries, _ := ListPlans("routine_vision", o)
			for _, s := range summaries {
				if s.CarrierID == "car281317" && s.CarrierName != "EMBOSS SPECIALTY" {
					t.Errorf("%s: carrier name = %q", s.PlanID, s.CarrierName)
				}
			}
		})
	}
}

func TestEveryPlanNameResolvesToItsPlan(t *testing.T) {
	for _, list := range catalog {
		for _, officeID := range list.Offices {
			o := office(t, officeID)
			for _, p := range list.Plans {
				if !officeSupports(o, p.Coverage) {
					continue
				}
				byID := DecidePlan(p.ID, p.Coverage, o, adultDOB, testNow)
				for _, name := range planNames(p) {
					if byName := DecideInsurance(name, p.Coverage, o, adultDOB, testNow); byName.PlanID != p.ID || byName.Outcome != byID.Outcome {
						t.Errorf("%s %q: by name %+v, by id %+v", officeID, name, byName, byID)
					}
				}
			}
		}
	}
}

func TestRealDataAetnaBetterAtSouthFloridaOffices(t *testing.T) {
	hollywood := DecideInsurance("Aetna Better", "medical", office(t, "hollywood"), adultDOB, testNow)
	sweetwater := DecideInsurance("Aetna Better", "medical", office(t, "sweetwater"), adultDOB, testNow)
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
		d := DecideInsurance("Aetna", "medical", o, adultDOB, testNow)
		if d.Outcome != "needs_clarification" || len(d.Options) < 2 {
			t.Fatalf("%s: decision = %+v", officeID, d)
		}
		for _, option := range d.Options {
			if again := DecideInsurance(option.Label, "medical", o, adultDOB, testNow); again.PlanID != option.PlanID {
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
		if d := DecideInsurance(heard, "medical", office(t, "hollywood"), adultDOB, testNow); d.PlanID != planID {
			t.Errorf("%q: decision = %+v", heard, d)
		}
	}
}

func TestRealDataSpringHillHumanaMedicareKeepsItsCarrier(t *testing.T) {
	d := DecideInsurance("Humana Medicare", "medical", office(t, "spring_hill"), adultDOB, testNow)
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
		d := DecideInsurance("Simply Medicaid", c.coverage, office(t, c.office), adultDOB, testNow)
		if d.Outcome != "accepted" || d.CallerNotice != simplySubscriberNotice || !strings.HasSuffix(d.Answer, simplySubscriberNotice) {
			t.Errorf("%s %s: decision = %+v", c.office, c.coverage, d)
		}
	}
}

const simplySubscriberNotice = "For Simply Medicaid, we need the subscriber number on your card that starts with 7, not the Medicaid number that starts with 8."

func TestRealDataLeonRoutesToTheLeonCoordinator(t *testing.T) {
	const notice = "Leon Medical Center patients schedule through their Leon coordinator."
	for _, officeID := range []string{"hollywood", "sweetwater"} {
		t.Run(officeID, func(t *testing.T) {
			o := office(t, officeID)
			chart := DecideChartInsurance(domain.PatientDemographics{CarrierID: "car40908", CarrierName: "LEON MEDICAL CENTER HEALTH PLAN"}, "medical", o, adultDOB, testNow)
			said := DecideInsurance("Leon Medical Center", "medical", o, adultDOB, testNow)
			for _, d := range []InsuranceDecision{chart, said} {
				if d.PlanID != "leon-medical-center-health-plan" || d.Participation != "accepted" || d.CanSchedule || d.Reason != "requirement" || d.CallerNotice != notice {
					t.Fatalf("decision = %+v", d)
				}
			}
		})
	}
}

func TestRealDataSelfPayPhrasingsResolveToSelfPay(t *testing.T) {
	selfPayPlan := map[string]string{"medical": "self-pay-medical", "routine_vision": "self-pay-routine-vision"}
	for _, c := range []struct{ office, coverage string }{
		{"hollywood", "medical"}, {"hollywood", "routine_vision"},
		{"sweetwater", "medical"}, {"sweetwater", "routine_vision"},
		{"north_miami_beach_optical", "routine_vision"},
		{"spring_hill", "medical"}, {"spring_hill", "routine_vision"},
		{"crystal_river", "medical"},
	} {
		o := office(t, c.office)
		want := DecidePlan(selfPayPlan[c.coverage], c.coverage, o, adultDOB, testNow)
		if want.Outcome != "accepted" || !want.SelfPay {
			t.Fatalf("%s %s: self-pay plan = %+v", c.office, c.coverage, want)
		}
		for _, heard := range []string{
			"I don't have insurance", "I'm paying out of pocket", "no insurance", "uninsured", "self pay", "I'll pay myself",
			"I have no insurance", "I do not have insurance", "I don't have any insurance", "She doesn't have insurance", "private pay",
		} {
			if got := DecideInsurance(heard, c.coverage, o, adultDOB, testNow); !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s %q: got plan %q reason %q", c.office, c.coverage, heard, got.PlanID, got.Reason)
			}
		}
	}
}

func TestRealDataSelfPayPhrasingsDoNotOverrideANamedPlan(t *testing.T) {
	hollywood := office(t, "hollywood")
	for heard, planID := range map[string]string{
		"Seminole Tribe of Florida Self-Funded Health Plan":      "seminole-tribe-medical",
		"UnitedHealthcare Community Plan":                        "united-healthcare-medicaid-non-par",
		"I have no insurance card, but it's Aetna Better Health": "aetna-better-health-medicaid-medical",
	} {
		if d := DecideInsurance(heard, "medical", hollywood, adultDOB, testNow); d.PlanID != planID || d.SelfPay {
			t.Errorf("%q: decision = %+v", heard, d)
		}
	}
	if d := DecideInsurance("no insurance, just Medicaid", "medical", hollywood, adultDOB, testNow); d.SelfPay || d.Reason != "ask_full_name" {
		t.Errorf("Medicaid: decision = %+v", d)
	}
}
