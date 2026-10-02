package insurance

import (
	"fmt"
	"slices"
	"testing"
)

func TestDecideInsuranceMatchesWhatTheCallerSaid(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	hollywood := office(t, "hollywood")
	tests := []struct {
		name    string
		heard   string
		outcome string
		planID  string
	}{
		{"exact label", "Aetna Better Health", "accepted", "aetna-better-health"},
		{"exact alias with punctuation", "aetna-better health (medicaid)", "accepted", "aetna-better-health"},
		{"typo", "Aetna Bettr Helth", "accepted", "aetna-better-health"},
		{"extra words", "I have Aetna Better Health through my job", "accepted", "aetna-better-health"},
		{"fragment shared by plans that end the same", "Aetna Better", "accepted", "aetna-better-health"},
		{"fuzzy fragment", "Aetna Medicar", "accepted", "aetna-medicare"},
		{"self pay", "cash", "accepted", "self-pay"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideInsurance(tc.heard, "medical", hollywood, adultDOB)
			if d.Outcome != tc.outcome || d.PlanID != tc.planID || len(d.Options) != 0 {
				t.Fatalf("decision = %+v", d)
			}
		})
	}
}

func TestDecideInsuranceAsksWithOptionsWhenPlansEndDifferently(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	d := DecideInsurance("Aetna", "medical", office(t, "hollywood"), adultDOB)
	want := []InsuranceOption{
		{PlanID: "aetna-medicare", Label: "Aetna Medicare"},
		{PlanID: "aetna-commercial", Label: "Aetna Commercial"},
		{PlanID: "aetna-better-health", Label: "Aetna Better Health"},
	}
	if d.Outcome != "needs_clarification" || d.Participation != "unknown" || d.PlanID != "" || !slices.Equal(d.Options, want) {
		t.Fatalf("decision = %+v", d)
	}
	if d.Answer != "needs_input: Which of these is on your card: Aetna Medicare, Aetna Commercial, or Aetna Better Health?" {
		t.Fatalf("answer = %q", d.Answer)
	}
	for _, option := range d.Options {
		if again := DecideInsurance(option.Label, "medical", office(t, "hollywood"), adultDOB); again.PlanID != option.PlanID {
			t.Fatalf("option %q resolved to %q", option.Label, again.PlanID)
		}
	}
}

func TestDecideInsuranceLimitsOptions(t *testing.T) {
	plans := []plan{}
	for i, id := range []string{"one", "two", "three", "four", "five"} {
		plans = append(plans, plan{ID: "acme-" + id, Label: "Acme " + id, Coverage: "medical", CarrierID: fmt.Sprintf("car%d", i+1), Doctors: bach("yes")})
	}
	useSyntheticCatalog(t, plans, nil, nil)
	d := DecideInsurance("Acme", "medical", office(t, "hollywood"), adultDOB)
	if d.Outcome != "needs_clarification" || len(d.Options) != 0 || d.Answer != answerAskFullName {
		t.Fatalf("decision = %+v", d)
	}
	useSyntheticCatalog(t, plans[:maxOptions], nil, nil)
	d = DecideInsurance("Acme", "medical", office(t, "hollywood"), adultDOB)
	if len(d.Options) != maxOptions || d.Answer != "needs_input: Which of these is on your card: Acme one, Acme two, Acme four, or Acme three?" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestDecideInsuranceAsksForTheCardWhenNothingSpecificMatches(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	for _, heard := range []string{"", "health plan", "my insurance", "Medicare", "Aetna Betr Helth", "Blue Cross"} {
		d := DecideInsurance(heard, "medical", office(t, "hollywood"), adultDOB)
		if d.Outcome != "needs_clarification" || d.Answer != answerAskCard || len(d.Options) != 0 {
			t.Fatalf("%q: decision = %+v", heard, d)
		}
	}
}

func TestDecideInsuranceAsksWhenThePlanIsOnlyInTheOtherCoverage(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	for _, c := range []struct{ heard, coverage string }{{"VSP", "medical"}, {"Aetna Medicare", "routine_vision"}} {
		d := DecideInsurance(c.heard, c.coverage, office(t, "hollywood"), adultDOB)
		if d.Outcome != "needs_clarification" || d.Participation != "unknown" || d.PlanID != "" || d.Answer != answerAskCard {
			t.Fatalf("%q %s: decision = %+v", c.heard, c.coverage, d)
		}
	}
}

func TestTokensNormalizeAndDropFillerWords(t *testing.T) {
	got := tokens("I'm with the AETNA Medicare HMO & PPO card")
	if !slices.Equal(got, []string{"aetna", "medicare", "hmo", "ppo"}) {
		t.Fatalf("tokens = %q", got)
	}
}

func TestTokenPointsUsesEditDistanceByLength(t *testing.T) {
	tests := []struct {
		heard, name string
		points      int
	}{
		{"aetna", "aetna", exactTokenPoints},
		{"helth", "health", fuzzyTokenPoints},
		{"betr", "better", 0},
		{"cgna", "cigna", 0},
		{"medicaree", "medicare", fuzzyTokenPoints},
		{"healthcre", "healthcare", fuzzyTokenPoints},
		{"helthcre", "healthcare", 0},
		{"humnaaa", "humana", 0},
	}
	for _, tc := range tests {
		if got := tokenPoints(tc.heard, tc.name); got != tc.points {
			t.Errorf("tokenPoints(%q, %q) = %d, want %d", tc.heard, tc.name, got, tc.points)
		}
	}
}
