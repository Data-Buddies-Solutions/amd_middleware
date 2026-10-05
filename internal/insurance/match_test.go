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
		{"fuzzy fragment", "Aetna Medicaree", "accepted", "aetna-medicare"},
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
	for i, id := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		plans = append(plans, plan{ID: "acme-" + id, Label: "Acme " + id, Coverage: "medical", CarrierID: fmt.Sprintf("car%d", i+1), Doctors: bach("yes")})
	}
	useSyntheticCatalog(t, plans, nil, nil)
	d := DecideInsurance("Acme", "medical", office(t, "hollywood"), adultDOB)
	if d.Outcome != "needs_clarification" || len(d.Options) != 0 || d.Answer != answerAskFullName {
		t.Fatalf("decision = %+v", d)
	}
	useSyntheticCatalog(t, plans[:maxOptions], nil, nil)
	d = DecideInsurance("Acme", "medical", office(t, "hollywood"), adultDOB)
	if len(d.Options) != maxOptions || d.Answer != "needs_input: Which of these is on your card: Acme alpha, Acme bravo, Acme delta, or Acme charlie?" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestDecideInsuranceAsksForTheCardWhenNothingSpecificMatches(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	for _, heard := range []string{"", "health plan", "my insurance", "Blue Cross", "the second one", "Tier 1"} {
		d := DecideInsurance(heard, "medical", office(t, "hollywood"), adultDOB)
		if d.Outcome != "needs_clarification" || d.Answer != answerAskCard || len(d.Options) != 0 {
			t.Fatalf("%q: decision = %+v", heard, d)
		}
	}
}

func TestDecideInsuranceAsksForTheFullNameWhenOnlyTheProgramIsNamed(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	for _, heard := range []string{"Medicare", "Medicaid", "Medicare Advantage", "my Medicaid card"} {
		d := DecideInsurance(heard, "medical", office(t, "hollywood"), adultDOB)
		if d.Outcome != "needs_clarification" || d.Answer != answerAskFullName || len(d.Options) != 0 {
			t.Fatalf("%q: decision = %+v", heard, d)
		}
	}
}

func TestDecideInsuranceAsksWhenThePlanIsOnlyInTheOtherCoverage(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	for _, c := range []struct{ heard, coverage, answer string }{{"VSP", "medical", answerAskCard}, {"Aetna Medicare", "routine_vision", answerAskFullName}} {
		d := DecideInsurance(c.heard, c.coverage, office(t, "hollywood"), adultDOB)
		if d.Outcome != "needs_clarification" || d.Participation != "unknown" || d.PlanID != "" || d.Answer != c.answer {
			t.Fatalf("%q %s: decision = %+v", c.heard, c.coverage, d)
		}
	}
}

func TestTokensNormalizeAndDropFillerWords(t *testing.T) {
	got := tokens("I'm with the AETNA Medicare HMO & PPO card, just that one")
	if !slices.Equal(got, []string{"aetna", "medicare", "hmo", "ppo"}) {
		t.Fatalf("tokens = %q", got)
	}
	for _, heard := range []string{"I care", "i-care", "iCare"} {
		if got := tokens(heard); !slices.Equal(got, []string{"icare"}) {
			t.Errorf("tokens(%q) = %q", heard, got)
		}
	}
}

func TestCallerWordsFixCommonWordsAndDropUnknownWords(t *testing.T) {
	vocabulary := map[string]bool{"aetna": true, "better": true, "health": true, "medicare": true, "medicaid": true}
	tests := []struct {
		heard string
		want  []string
	}{
		{"Aetna Bettr Helth", []string{"aetna", "bettr", "health"}},
		{"Aetna Medicaree", []string{"aetna", "medicare"}},
		{"Aetna Medicaide", []string{"aetna", "medicaid"}},
		{"Aetna Medicade", []string{"aetna"}},
		{"Aetna Medicar", []string{"aetna"}},
		{"Aetna from work", []string{"aetna"}},
		{"Aetna PPO", []string{"aetna", "ppo"}},
	}
	for _, tc := range tests {
		if got := callerWords(tokens(tc.heard), vocabulary); !slices.Equal(got, tc.want) {
			t.Errorf("callerWords(%q) = %q, want %q", tc.heard, got, tc.want)
		}
	}
}

func TestTokenPointsUsesEditDistanceByLength(t *testing.T) {
	tests := []struct {
		heard, name string
		points      int
	}{
		{"aetna", "aetna", exactTokenPoints},
		{"helth", "health", 0},
		{"medicaid", "medicare", 0},
		{"humanna", "humana", fuzzyTokenPoints},
		{"betr", "better", 0},
		{"cgna", "cigna", 0},
		{"medicaree", "medicare", 0},
		{"healthcre", "healthcare", 0},
		{"staywel", "staywell", fuzzyTokenPoints},
		{"devotted", "devoted", fuzzyTokenPoints},
		{"humnaaa", "humana", 0},
	}
	for _, tc := range tests {
		if got := tokenPoints(tc.heard, tc.name); got != tc.points {
			t.Errorf("tokenPoints(%q, %q) = %d, want %d", tc.heard, tc.name, got, tc.points)
		}
	}
}
