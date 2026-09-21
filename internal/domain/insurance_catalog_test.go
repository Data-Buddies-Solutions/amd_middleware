package domain

import (
	"strings"
	"testing"
)

func TestMedicalCatalogPreservesOfficeExclusions(t *testing.T) {
	for _, tc := range []struct{ office, plan string }{
		{"Spring Hill", "Florida Blue Select"}, {"Spring Hill", "Florida Blue BlueSelect"},
		{"Spring Hill", "Care Plus"}, {"Spring Hill", "Humana Gold Plus"},
		{"Spring Hill", "Aetna EPO"}, {"Spring Hill", "Preferred Care Partners"},
		{"Crystal River", "Ambetter"}, {"Crystal River", "Molina Medicaid"},
		{"Hollywood", "Molina Marketplace"}, {"Hollywood", "Cigna Local Plus"},
	} {
		office, _ := ResolveOffice(tc.office)
		d := DecideInsurance(tc.plan, "medical", office, "")
		if d.Outcome != "not_accepted" || (d.Participation == "accepted") || d.CanSchedule {
			t.Errorf("office exclusion lost: %+v => %+v", tc, d)
		}
	}
}

func TestAcceptedNamedProductsHaveRegistrationMappings(t *testing.T) {
	office, _ := ResolveOffice("Spring Hill")
	for _, q := range []string{"Devoted Medicare HMO", "Humana Medicare PPO"} {
		d := DecideInsurance(q, "medical", office, "")
		if d.Outcome != "accepted" || d.Participation != "accepted" || d.CarrierID == "" || d.Routing == "" || !d.CanSchedule {
			t.Errorf("specific product inherited a parent mapping: %s %+v", q, d)
		}
	}
}

func TestSharedMedicalAliasesUseThePlanIdentity(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, tc := range []struct{ input, code string }{
		{"Florida Blue PPO", "FLO01"}, {"BCBS PPO", "FLO01"},
		{"Meritain Health - Aetna", "MERI1"}, {"Sunshine Health", "AMBE1"},
		{"Florida Complete Care - Medicare Medical ONLY", "ICA01"},
		{"Aetna Commercial", "AET07"}, {"Aetna Medicare PPO", "ICA01"},
		{"Self-pay", "SELF"}, {"UMR (United Health One)", "UNIT3"},
	} {
		d := DecideInsurance(tc.input, "medical", office, "")
		if d.Participation != "accepted" || d.CarrierCode != tc.code {
			t.Errorf("alias lost identity: %s %+v", tc.input, d)
		}
	}
}

func TestMedicalAliasesHaveOneOwner(t *testing.T) {
	owners := map[string]string{}
	for _, p := range medicalPlans {
		for _, alias := range append([]string{p.Name}, p.Aliases...) {
			key := insuranceNormalize(alias)
			if owner, ok := owners[key]; ok && owner != p.Name {
				t.Errorf("alias %q belongs to both %s and %s", alias, owner, p.Name)
			}
			owners[key] = p.Name
		}
	}
}

func TestAMDDirectoryRoundTripUsesConfirmedProductWithoutRelaxingRestrictions(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	for _, tc := range []struct{ plan, id, name string }{
		{"Aetna Commercial", "car40887", "AETNA"},
		{"United Healthcare NHP HMO Access", "car40923", "UNITED HEALTHCARE"},
		{"Humana Medicare PPO", "car303062", "HUMANA PPO POS"},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			initial := DecideInsurance(tc.plan, "medical", office, "")
			if initial.Participation != "accepted" || !initial.CanSchedule {
				t.Fatalf("initial product blocked: %+v", initial)
			}
			chart := PatientDemographics{CarrierID: tc.id, CarrierName: tc.name}
			got := DecideChartInsurance(chart, initial.CanonicalPlan, "medical", office, "")
			if !got.CanSchedule || got.CanonicalPlan != initial.CanonicalPlan {
				t.Fatalf("AMD round trip lost confirmed product: %+v", got)
			}
			chart.CarrierID = "car999999"
			if DecideChartInsurance(chart, tc.plan, "medical", office, "").CanSchedule {
				t.Fatal("wrong chart carrier accepted")
			}
			chart.CarrierID = tc.id
			chart.CarrierName = "Unverified carrier display"
			if DecideChartInsurance(chart, tc.plan, "medical", office, "").CanSchedule {
				t.Fatal("unknown display relabeled")
			}
		})
	}
	chart := PatientDemographics{CarrierID: "car40923", CarrierName: "UNITED HEALTHCARE"}
	for _, plan := range []string{"United Healthcare Individual Exchange"} {
		if d := DecideChartInsurance(chart, plan, "medical", office, ""); d.CanSchedule {
			t.Fatalf("clarification or referral bypassed: %+v", d)
		}
	}
	chart.CarrierName = "United Healthcare NHP HMO Only"
	if DecideChartInsurance(chart, "United Healthcare NHP HMO Access", "medical", office, "").CanSchedule {
		t.Fatal("explicit chart restriction overridden")
	}
}

func TestRegistrationPermissionIsExplicitWhenSchedulingIsHeld(t *testing.T) {
	office, _ := ResolveOffice("Hollywood")
	d := DecideInsurance("Humana Medicaid HMO", "medical", office, "")
	if d.Participation != "accepted" || d.CanSchedule || !strings.Contains(d.Answer, "prior authorization before scheduling") {
		t.Fatalf("unclear next action: %+v", d)
	}
}

func TestPremierEyeCareChartRoundTrip(t *testing.T) {
	office, _ := ResolveOffice("North Miami Beach Optical")
	initial := DecideInsurance("Devoted", "routine_vision", office, "01/02/1980")
	if !initial.CanSchedule || initial.CanonicalPlan != "Premier" {
		t.Fatalf("Devoted acceptance: %+v", initial)
	}
	chart := PatientDemographics{CarrierID: initial.CarrierID, CarrierName: "PREMIER EYE CARE"}
	for _, plan := range []string{"", "Premier", "Devoted"} {
		got := DecideChartInsurance(chart, plan, "routine_vision", office, "01/02/1980")
		if !got.CanSchedule || got.CanonicalPlan != "Premier" {
			t.Errorf("chart label lost accepted coverage for %q: %+v", plan, got)
		}
	}
	for _, plan := range []string{"VSP", "Eye Care", "Premier Eye Care or VSP"} {
		if got := DecideChartInsurance(chart, plan, "routine_vision", office, "01/02/1980"); got.CanSchedule {
			t.Errorf("conflicting plan %q accepted: %+v", plan, got)
		}
	}
	chart.CarrierID = "car-wrong"
	if DecideChartInsurance(chart, "Premier", "routine_vision", office, "01/02/1980").CanSchedule {
		t.Fatal("mismatched chart carrier accepted")
	}
}

func TestVisionChartIdentityDoesNotDependOnDirectoryLabel(t *testing.T) {
	office, _ := ResolveOffice("North Miami Beach Optical")
	for _, plan := range []string{"Devoted", "VSP", "EyeMed", "SunHealth"} {
		initial := DecideInsurance(plan, "routine_vision", office, "01/02/1980")
		for _, label := range []string{"", "Renamed provider directory label", "PREMIER EYE CARE"} {
			chart := PatientDemographics{CarrierID: initial.CarrierID, CarrierName: label}
			got := DecideChartInsurance(chart, initial.CanonicalPlan, "routine_vision", office, "01/02/1980")
			if !got.CanSchedule || got.CarrierID != initial.CarrierID {
				t.Errorf("%s / %q: %+v", plan, label, got)
			}
		}
	}
	for _, id := range []string{"", "car-unknown", "car40916"} {
		got := DecideChartInsurance(PatientDemographics{CarrierID: id, CarrierName: "Premier"}, "Premier", "routine_vision", office, "01/02/1980")
		if got.CanSchedule {
			t.Errorf("unmapped carrier accepted: %s", id)
		}
	}
}

func TestVisionChartRoundTripPreservesEveryAcceptedAlias(t *testing.T) {
	office, _ := ResolveOffice("North Miami Beach Optical")
	for _, rule := range participationSources["SPRING_HILL_ROUTINE_VISION"] {
		for _, plan := range append([]string{rule.Display}, rule.Aliases...) {
			accepted := DecideInsurance(plan, "routine_vision", office, "01/02/1980")
			if !accepted.CanSchedule {
				continue
			}
			chart := PatientDemographics{CarrierID: accepted.CarrierID, CarrierName: "Provider directory label"}
			got := DecideChartInsurance(chart, accepted.CanonicalPlan, "routine_vision", office, "01/02/1980")
			if !got.CanSchedule || got.CarrierID != accepted.CarrierID {
				t.Errorf("accepted %q (%s) lost in chart round trip: %+v", plan, accepted.CanonicalPlan, got)
			}
		}
	}
}
