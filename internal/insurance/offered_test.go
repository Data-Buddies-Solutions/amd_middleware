package insurance

import (
	"slices"
	"testing"
)

func TestDecideOfferedAnswerResolvesAmongTheOfferedPlans(t *testing.T) {
	springHill := office(t, "spring_hill")
	asked := DecideInsurance("Aetna", "medical", springHill, adultDOB)
	offered := []string{}
	for _, option := range asked.Options {
		offered = append(offered, option.PlanID)
	}
	if !slices.Equal(offered, []string{"aetna-epo", "aetna-hmo", "aetna-medicare"}) {
		t.Fatalf("options = %+v", asked.Options)
	}
	tests := []struct {
		heard   string
		planID  string
		options []string
	}{
		{"Medicare", "aetna-medicare", nil},
		{"uh the Medicare one please", "aetna-medicare", nil},
		{"HMO", "aetna-hmo", nil},
		{"Aetna EPO", "aetna-epo", nil},
		{"Aetna Medicare", "aetna-medicare", nil},
		{"um yes", "", offered},
		{"Aetna", "", offered},
		{"Humana Medicare", "humana-medicare", nil},
	}
	for _, tc := range tests {
		d := DecideOfferedAnswer(tc.heard, offered, "medical", springHill, adultDOB)
		got := []string{}
		for _, option := range d.Options {
			got = append(got, option.PlanID)
		}
		if d.PlanID != tc.planID || !slices.Equal(got, append([]string{}, tc.options...)) {
			t.Errorf("%q: decision = %+v", tc.heard, d)
		}
	}
}

func TestDecideOfferedAnswerNarrowsOrFallsBack(t *testing.T) {
	hollywood := office(t, "hollywood")
	blue := []string{"florida-blue-hmo", "florida-blue-medicare-hmo-medical"}
	if d := DecideOfferedAnswer("It's Florida Blue HMO", blue, "medical", hollywood, adultDOB); d.PlanID != "florida-blue-hmo" {
		t.Fatalf("exact offered name = %+v", d)
	}
	if d := DecideOfferedAnswer("the HMO", blue, "medical", hollywood, adultDOB); len(d.Options) != 2 {
		t.Fatalf("ambiguous answer = %+v", d)
	}
	if d, plain := DecideOfferedAnswer("Sunshine Health", blue, "medical", hollywood, adultDOB), DecideInsurance("Sunshine Health", "medical", hollywood, adultDOB); d.PlanID != plain.PlanID || d.PlanID == "" {
		t.Fatalf("another plan = %+v, plain = %+v", d, plain)
	}
	if d, plain := DecideOfferedAnswer("Aetna", nil, "medical", hollywood, adultDOB), DecideInsurance("Aetna", "medical", hollywood, adultDOB); d.Answer != plain.Answer {
		t.Fatalf("no offered plans = %+v", d)
	}
}
