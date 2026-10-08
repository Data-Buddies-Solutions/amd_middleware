package insurance

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"advancedmd-token-management/internal/domain"
)

func dobYearsAgo(years int) string {
	return time.Now().AddDate(-years, 0, -1).Format("01/02/2006")
}

func springHillPlans() []plan {
	return []plan{
		{ID: "all-three", Label: "All Three", Coverage: "medical", CarrierID: "car1", Doctors: map[string]string{"Dr. Austin Bach": "yes", "Dr. Joseph Licht": "yes", "Dr. Noel": "yes"}},
		{ID: "licht-only", Label: "Licht Only", Coverage: "medical", CarrierID: "car2", Doctors: map[string]string{"Dr. Joseph Licht": "yes", "Dr. Noel": "no"}},
		{ID: "pending-plan", Label: "Pending Plan", Coverage: "medical", CarrierID: "car3", Doctors: map[string]string{"Dr. Austin Bach": "pending"}, CallerNotice: "Staff will call you back."},
		{ID: "prior-auth-plan", Label: "Prior Auth Plan", Coverage: "medical", CarrierID: "car4", Doctors: bach("yes"), Requirements: []planRequirement{{Kind: "prior_authorization", Channel: "availity"}}},
		{ID: "referral-plan", Label: "Referral Plan", Coverage: "medical", CarrierID: "car5", Doctors: bach("yes"), Requirements: []planRequirement{{Kind: "pcp_referral"}}},
		{ID: "verify-plan", Label: "Verify Plan", Coverage: "medical", CarrierID: "car5", Doctors: bach("yes"), Requirements: []planRequirement{{Kind: "staff_verify"}}},
		{ID: "otero-vision", Label: "Otero Vision", Coverage: "routine_vision", CarrierID: "car1", Doctors: map[string]string{"Dr. Melissa Otero": "yes"}, CallerNotice: "Bring your card."},
	}
}

func TestDecidePlanFiltersOfficeDoctorsByThePlan(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), springHillPlans(), nil)
	springHill := office(t, "spring_hill")
	tests := []struct {
		name     string
		planID   string
		coverage string
		office   string
		dob      string
		outcome  string
		allowed  []string
	}{
		{"all medical doctors", "all-three", "medical", "spring_hill", adultDOB, "accepted", []string{"Dr. Bach", "Dr. Licht", "Dr. Noel"}},
		{"plan doctors only", "licht-only", "medical", "spring_hill", adultDOB, "accepted", []string{"Dr. Licht"}},
		{"pediatric routing keeps Bach", "all-three", "medical", "spring_hill", dobYearsAgo(10), "accepted", []string{"Dr. Bach"}},
		{"pediatric routing without plan doctor", "licht-only", "medical", "spring_hill", dobYearsAgo(10), "needs_staff_task", []string{}},
		{"no DOB keeps age-limited doctors", "vsp", "routine_vision", "hollywood", "", "accepted", []string{"Dr. Farnan", "Dr. Vidal"}},
		{"no DOB keeps every medical doctor", "all-three", "medical", "spring_hill", "", "accepted", []string{"Dr. Bach", "Dr. Licht", "Dr. Noel"}},
		{"child too young for every accepting doctor", "vsp", "routine_vision", "hollywood", dobYearsAgo(3), "needs_staff_task", []string{}},
		{"routine vision uses optical doctors", "otero-vision", "routine_vision", "spring_hill", adultDOB, "accepted", []string{"Dr. Otero"}},
		{"vision doctors with pending excluded", "vsp", "routine_vision", "hollywood", adultDOB, "accepted", []string{"Dr. Farnan", "Dr. Vidal"}},
		{"doctor minimum age", "vsp", "routine_vision", "hollywood", dobYearsAgo(6), "accepted", []string{"Dr. Farnan"}},
		{"other office doctors", "vsp", "routine_vision", "sweetwater", adultDOB, "accepted", []string{"Dr. Casas", "Dr. Farnan"}},
		{"doctor says no", "aetna-commercial", "medical", "hollywood", adultDOB, "not_accepted", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := DecidePlan(tc.planID, tc.coverage, office(t, tc.office), tc.dob)
			if d.Outcome != tc.outcome || d.PlanID != tc.planID || !slices.Equal(d.AllowedProviders, tc.allowed) {
				t.Fatalf("decision = %+v", d)
			}
			if d.CanSchedule != (tc.outcome == "accepted") {
				t.Fatalf("canSchedule = %v", d.CanSchedule)
			}
		})
	}
	for _, tc := range []struct{ planID, coverage, office, dob string }{
		{"licht-only", "medical", "spring_hill", dobYearsAgo(10)},
		{"vsp", "routine_vision", "hollywood", dobYearsAgo(3)},
	} {
		d := DecidePlan(tc.planID, tc.coverage, office(t, tc.office), tc.dob)
		if d.Participation != "accepted" || d.Answer != answerNoDoctorForAge {
			t.Fatalf("%+v: child decision = %+v", tc, d)
		}
	}
	if d := DecidePlan("otero-vision", "routine_vision", springHill, adultDOB); d.Answer != "success: Yes, we accept Otero Vision. Bring your card." {
		t.Fatalf("answer = %q", d.Answer)
	}
}

func TestDecidePlanSendsPendingAndRequirementsToStaff(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), springHillPlans(), nil)
	springHill := office(t, "spring_hill")

	pending := DecidePlan("pending-plan", "medical", springHill, adultDOB)
	if pending.Outcome != "needs_staff_task" || pending.Participation != "unknown" || pending.CanSchedule ||
		pending.Answer != "blocked: The office needs to confirm this coverage. Staff will call you back." {
		t.Fatalf("pending = %+v", pending)
	}

	auth := DecidePlan("prior-auth-plan", "medical", springHill, adultDOB)
	want := []InsuranceRequirement{{Kind: "prior_authorization", Channel: "availity", Verification: "unverified"}}
	if auth.Outcome != "needs_staff_task" || auth.Participation != "accepted" || auth.CanSchedule ||
		!slices.Equal(auth.Requirements, want) || auth.Answer != answerPriorAuth {
		t.Fatalf("prior auth = %+v", auth)
	}

	verify := DecidePlan("verify-plan", "medical", springHill, adultDOB)
	if verify.Outcome != "needs_staff_task" || verify.Participation != "accepted" || verify.CanSchedule || verify.Answer != answerStaffVerify {
		t.Fatalf("staff verify = %+v", verify)
	}

	referral := DecidePlan("referral-plan", "medical", springHill, adultDOB)
	if referral.Outcome != "needs_staff_task" || referral.Answer != answerReferral {
		t.Fatalf("referral = %+v", referral)
	}
}

func TestDecidePlanHonorsOnlyOffices(t *testing.T) {
	plans := append(syntheticSouthFlorida(), plan{ID: "miami-dade-plan", Label: "Miami Dade Plan", Coverage: "medical", CarrierID: "car1", Doctors: bach("yes"), OnlyOffices: []string{"sweetwater"}})
	useSyntheticCatalog(t, plans, nil, nil)
	if d := DecidePlan("miami-dade-plan", "medical", office(t, "sweetwater"), adultDOB); d.Outcome != "accepted" {
		t.Fatalf("sweetwater = %+v", d)
	}
	if d := DecidePlan("miami-dade-plan", "medical", office(t, "hollywood"), adultDOB); d.Outcome != "not_accepted" || d.Answer != answerNotAccepted {
		t.Fatalf("hollywood = %+v", d)
	}
}

func TestDecidePlanRequiresThePlanInTheOfficeListAndCoverage(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), springHillPlans(), nil)
	for _, tc := range []struct{ planID, coverage, office string }{
		{"all-three", "medical", "hollywood"},
		{"vsp", "medical", "hollywood"},
		{"missing", "medical", "hollywood"},
		{"", "medical", "hollywood"},
	} {
		d := DecidePlan(tc.planID, tc.coverage, office(t, tc.office), adultDOB)
		if d.Outcome != "needs_clarification" || d.PlanID != "" || d.Answer != answerAskCard {
			t.Fatalf("%+v: decision = %+v", tc, d)
		}
	}
}

func TestDecisionsRejectUnsupportedCoverage(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	d := DecideInsurance("Aetna Medicare", "medical", office(t, "north_miami_beach_optical"), adultDOB)
	if d.Outcome != "not_accepted" || d.Answer != answerOfficeNoCoverage {
		t.Fatalf("office without medical = %+v", d)
	}
	d = DecidePlan("aetna-medicare", "dental", office(t, "hollywood"), adultDOB)
	if d.Outcome != "needs_clarification" || d.Answer != answerAskCoverage {
		t.Fatalf("unknown coverage = %+v", d)
	}
}

func TestInsuranceDecisionJSON(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), nil, nil)
	accepted, err := json.Marshal(DecideInsurance("Aetna Medicare", "medical", office(t, "hollywood"), adultDOB))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"planId":"aetna-medicare"`, `"canonicalPlan":"Aetna Medicare"`, `"carrierCode":"AET07"`, `"carrierId":"car40887"`, `"allowedProviders":["Dr. Bach"]`, `"eligibility":"not_checked"`} {
		if !strings.Contains(string(accepted), field) {
			t.Fatalf("missing %s in %s", field, accepted)
		}
	}
	asked, err := json.Marshal(DecideInsurance("Aetna", "medical", office(t, "hollywood"), adultDOB))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(asked), `"options":[{"planId":"aetna-medicare","label":"Aetna Medicare"}`) {
		t.Fatalf("options missing in %s", asked)
	}
	for _, body := range []string{string(accepted), string(asked)} {
		if strings.Contains(body, "routing") || strings.Contains(body, "credentialedProviders") {
			t.Fatalf("dropped field present in %s", body)
		}
	}
	if strings.Contains(string(accepted), "options") {
		t.Fatalf("options present without a question: %s", accepted)
	}
}

func TestDecisionsNameTheirReasonAndCallerNotice(t *testing.T) {
	useSyntheticCatalog(t, syntheticSouthFlorida(), springHillPlans(), nil)
	springHill, hollywood := office(t, "spring_hill"), office(t, "hollywood")
	tests := []struct {
		name   string
		d      InsuranceDecision
		reason string
		notice string
	}{
		{"accepted", DecidePlan("all-three", "medical", springHill, adultDOB), "accepted", ""},
		{"accepted with notice", DecidePlan("otero-vision", "routine_vision", springHill, adultDOB), "accepted", "Bring your card."},
		{"pending", DecidePlan("pending-plan", "medical", springHill, adultDOB), "pending_confirmation", "Staff will call you back."},
		{"requirement", DecidePlan("prior-auth-plan", "medical", springHill, adultDOB), "requirement", ""},
		{"no doctor for age", DecidePlan("licht-only", "medical", springHill, dobYearsAgo(10)), "no_provider_for_age", ""},
		{"not accepted", DecidePlan("aetna-commercial", "medical", hollywood, adultDOB), "not_accepted", ""},
		{"unknown plan", DecidePlan("missing", "medical", hollywood, adultDOB), "ask_card", ""},
		{"unknown coverage", DecidePlan("aetna-medicare", "dental", hollywood, adultDOB), "ask_coverage", ""},
		{"office without coverage", DecideInsurance("Aetna Medicare", "medical", office(t, "north_miami_beach_optical"), adultDOB), "office_no_coverage", ""},
		{"several plans", DecideInsurance("Aetna", "medical", hollywood, adultDOB), "choose_plan", ""},
		{"program only", DecideInsurance("Medicare please", "medical", hollywood, adultDOB), "ask_full_name", ""},
		{"chart carrier unknown", DecideChartInsurance(domain.PatientDemographics{CarrierID: "car999"}, "medical", hollywood, adultDOB), "chart_unverified", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.d.Reason != tc.reason || tc.d.CallerNotice != tc.notice {
				t.Fatalf("reason = %q, callerNotice = %q; decision = %+v", tc.d.Reason, tc.d.CallerNotice, tc.d)
			}
		})
	}
	noticed, _ := json.Marshal(tests[1].d)
	plain, _ := json.Marshal(tests[0].d)
	if !strings.Contains(string(noticed), `"reason":"accepted","callerNotice":"Bring your card."`) || strings.Contains(string(plain), "callerNotice") {
		t.Fatalf("reason/callerNotice JSON: %s / %s", noticed, plain)
	}
}
