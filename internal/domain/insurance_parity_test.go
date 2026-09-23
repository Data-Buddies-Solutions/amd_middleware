package domain

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLegacyOfficeInsuranceOutcomes(t *testing.T) {
	InitRegistry("")
	check := func(t *testing.T, officeName, coverage, input, outcome string, priorAuth bool) {
		t.Helper()
		office, err := ResolveOffice(officeName)
		if err != nil {
			t.Fatal(err)
		}
		d := DecideInsurance(input, coverage, office, "01/02/1980")
		if d.Outcome != outcome {
			t.Errorf("outcome=%s; want %s; answer=%s", d.Outcome, outcome, d.Answer)
		}
		if d.Participation == "accepted" && (d.CarrierID == "" || d.Routing == "") {
			t.Errorf("accepted plan cannot be registered: %+v", d)
		}
		if priorAuth && (len(d.Requirements) == 0 || d.CanSchedule) {
			t.Errorf("lost legacy prior authorization: %+v", d)
		}
		if outcome == "accepted" && len(d.Requirements) > 0 {
			t.Errorf("added a scheduling hold: %+v", d.Requirements)
		}
	}
	for _, file := range []string{"insurance_legacy_outcomes.json", "insurance_mapping_outcomes.json"} {
		b, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Cases []struct {
				Office, Coverage, Outcome string
				PriorAuth                 bool
				Inputs                    []string
			}
			AliasExceptions []struct {
				Office, Coverage, Input, MappedPlan, OldOutcome, Outcome, Reason string
				PriorAuth                                                        bool
			}
		}
		if err := json.Unmarshal(b, &fixture); err != nil {
			t.Fatal(err)
		}
		for _, tc := range fixture.Cases {
			for _, input := range tc.Inputs {
				t.Run(tc.Office+"/"+tc.Coverage+"/"+input, func(t *testing.T) { check(t, tc.Office, tc.Coverage, input, tc.Outcome, tc.PriorAuth) })
			}
		}
		for _, tc := range fixture.AliasExceptions {
			t.Run(tc.Office+"/explicit-policy/"+tc.Input, func(t *testing.T) {
				if tc.Reason == "" || tc.MappedPlan == "" || tc.OldOutcome == tc.Outcome {
					t.Fatal("alias policy exception must explain the changed legacy match")
				}
				check(t, tc.Office, tc.Coverage, tc.Input, tc.Outcome, tc.PriorAuth)
			})
		}
	}
}
