package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCorrectedInsuranceIdentities(t *testing.T) {
	InitRegistry("")
	office, _ := ResolveOffice("Hollywood")
	for _, tc := range []struct{ plan, code, kind, channel string }{
		{"United Individual Exchange", "UNI20", "pcp_referral", "uhc_portal"},
		{"United AARP Medicare Complete/Medicare Advantage (HMO/LPPO)", "AARPM", "", ""},
		{"United Golden Rule", "GOL05", "", ""},
		{"United Oxford", "OX04", "", ""},
		{"United Shared Services", "UNIT9", "", ""},
		{"United Student Resources", "UHC STU", "", ""},
		{"United Surest", "BIND1", "", ""},
		{"United Global International Plan", "UNIT15", "vob_authorization", ""},
		{"Preferred Care Partners", "PRE04", "", ""},
		{"Humana Medicaid HMO", "HUM02", "prior_authorization", "availity"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			d := DecideInsurance(tc.plan, "medical", office, "01/02/1980")
			if d.CarrierCode != tc.code || d.Participation != "accepted" || d.Eligibility != "not_checked" {
				t.Fatalf("decision=%+v", d)
			}
			if tc.kind != "" && (len(d.Requirements) != 1 || d.Requirements[0] != (InsuranceRequirement{tc.kind, tc.channel, "unverified"})) {
				t.Fatalf("requirements=%+v", d.Requirements)
			}
			if d.CarrierID == "" {
				t.Fatal("accepted plan lacks carrier mapping")
			}
			if tc.kind != "" && d.CanSchedule {
				t.Fatal("requirement bypassed")
			}
			if tc.code == "PRE04" && (d.CarrierID != "car40916" || !d.CanSchedule) {
				t.Fatalf("PRE04=%+v", d)
			}
			b, _ := json.Marshal(d)
			if strings.Contains(string(b), "car40916") {
				t.Fatal("internal carrier ID exposed")
			}
		})
	}
}

func TestAmbiguousFamiliesNeverChooseProduct(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, plan := range []string{"United Golden Rule or United Oxford", "HUM03", "Clear Spring Health"} {
		d := DecideInsurance(plan, "medical", office, "")
		if (d.Participation == "accepted") || d.CanSchedule || d.CarrierID != "" {
			t.Fatalf("%s=%+v", plan, d)
		}
	}
}

func TestInsuranceOfficeScopeAndSimilarProducts(t *testing.T) {
	for _, tc := range []struct{ office, plan, coverage, outcome string }{
		{"Spring Hill", "Humana Gold Plus", "medical", "not_accepted"},
		{"Crystal River", "Humana PPO", "medical", "not_accepted"},
		{"Hollywood", "Humana Gold Plus", "medical", "accepted"},
		{"Sweetwater", "Humana Gold Plus", "routine_vision", "accepted"},
		{"Hollywood", "Florida Blue", "medical", "needs_clarification"},
		{"Hollywood", "Florida Blue HMO", "medical", "needs_staff_task"},
		{"Spring Hill", "Aetna EPO", "medical", "not_accepted"},
		{"Spring Hill", "I have Aetna Medicare PPO", "routine_vision", "accepted"},
		{"North Miami Beach Optical", "Aetna", "medical", "not_accepted"},
		{"Crystal River", "Self Pay", "routine_vision", "not_accepted"},
		{"Hollywood", "Preferred Care Partners", "routine_vision", "not_accepted"},
		{"Crystal River", "United Golden Rule", "medical", "accepted"},
	} {
		office, _ := ResolveOffice(tc.office)
		d := DecideInsurance(tc.plan, tc.coverage, office, "")
		if d.Outcome != tc.outcome {
			t.Errorf("%+v => %+v", tc, d)
		}
	}
	office, _ := ResolveOffice("Hollywood")
	nhp := DecideInsurance("United Healthcare NHP HMO Only", "medical", office, "")
	access := DecideInsurance("United Healthcare NHP HMO Access", "medical", office, "")
	if len(nhp.Requirements) != 0 || !nhp.CanSchedule || len(access.Requirements) != 0 {
		t.Fatalf("NHP=%+v access=%+v", nhp, access)
	}
}

func TestPRE04CredentialingAndChartBinding(t *testing.T) {
	for _, name := range []string{"Hollywood", "Sweetwater"} {
		office, _ := ResolveOffice(name)
		d := DecideChartInsurance(PatientDemographics{CarrierID: "car40916", CarrierName: "PREFERRED CARE PARTNERS"}, "", "medical", office, "01/02/1980")
		if d.CarrierCode != "PRE04" || len(d.CredentialedProviders) != 3 || !d.CanSchedule || len(d.AllowedProviders) != 1 || d.AllowedProviders[0] != "Dr. Bach" {
			t.Fatalf("%s=%+v", name, d)
		}
		d = DecideChartInsurance(PatientDemographics{CarrierID: "car40916", CarrierName: "Preferred Care Partners"}, "Aetna", "medical", office, "01/02/1980")
		if d.CanSchedule {
			t.Fatal("Caller correction silently scheduled against old PRE04 plan")
		}
		// Caller-selected Preferred Care cannot relabel an existing United attachment.
		d = DecideChartInsurance(PatientDemographics{CarrierID: "car40923", CarrierName: "United Healthcare"}, "Preferred Care Partners", "medical", office, "01/02/1980")
		if d.CanSchedule {
			t.Fatal("mismatched chart scheduled")
		}
	}
}

func TestDocumentNotesDoNotChangeLegacyParticipation(t *testing.T) {
	for _, tc := range []struct{ office, plan, coverage string }{
		{"Hollywood", "Aetna Better Health", "medical"},
		{"Hollywood", "Aetna Better Health", "routine_vision"},
		{"Hollywood", "Molina Medicaid", "medical"},
		{"Sweetwater", "Optimum Healthcare", "medical"},
		{"Sweetwater", "Freedom Health Medicare", "medical"},
		{"Sweetwater", "Care Plus", "medical"},
		{"Hollywood", "Aetna EPO", "medical"},
		{"Hollywood", "Multiplan", "medical"},
	} {
		office, _ := ResolveOffice(tc.office)
		d := DecideInsurance(tc.plan, tc.coverage, office, "")
		if d.Participation != "accepted" {
			t.Fatalf("document note changed participation: %+v => %+v", tc, d)
		}
	}

}
