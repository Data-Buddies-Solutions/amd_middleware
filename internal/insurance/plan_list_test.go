package insurance

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestListPlansSortsByLabelIgnoringCase(t *testing.T) {
	plans := append(syntheticSouthFlorida(), plan{ID: "lowercase-plan", Label: "aetna basic", Coverage: "medical", CarrierID: "car1", Doctors: bach("yes")})
	useSyntheticCatalog(t, plans, springHillPlans(), nil)
	got, ok := ListPlans("medical", office(t, "hollywood"))
	if !ok {
		t.Fatal("medical rejected")
	}
	labels := []string{}
	for _, s := range got {
		labels = append(labels, s.Label)
	}
	want := []string{"aetna basic", "Aetna Better Health", "Aetna Better Health Kids", "Aetna Commercial", "Aetna Medicare", "Self Pay"}
	if !slices.Equal(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
	if got[0].CarrierName != "ONE" || got[5].CarrierName != "SELF PAY" {
		t.Fatalf("carrier names = %q, %q", got[0].CarrierName, got[5].CarrierName)
	}
}

func TestListPlansMatchesPlanDecisionsAtEveryOffice(t *testing.T) {
	for _, list := range catalog {
		for _, officeID := range list.Offices {
			o := office(t, officeID)
			for _, coverage := range []string{"medical", "routine_vision"} {
				summaries, ok := ListPlans(coverage, o)
				if !ok {
					t.Fatalf("%s %s rejected", officeID, coverage)
				}
				if !officeSupports(o, coverage) {
					if len(summaries) != 0 {
						t.Fatalf("%s %s lists plans the office does not offer", officeID, coverage)
					}
					continue
				}
				want := 0
				for _, p := range list.Plans {
					if p.Coverage == coverage {
						want++
					}
				}
				if len(summaries) != want {
					t.Fatalf("%s %s: %d plans, want %d", officeID, coverage, len(summaries), want)
				}
				for i, s := range summaries {
					if i > 0 && compareLabels(summaries[i-1].Label, s.Label) > 0 {
						t.Fatalf("%s %s: %q listed before %q", officeID, coverage, summaries[i-1].Label, s.Label)
					}
					if slices.Contains(s.Names, s.Label) {
						t.Fatalf("%s %s: names of %s include the label", officeID, coverage, s.PlanID)
					}
					d := DecidePlan(s.PlanID, coverage, o, "")
					kinds := []string{}
					for _, r := range d.Requirements {
						kinds = append(kinds, r.Kind)
					}
					if s.Outcome != d.Outcome || !slices.Equal(s.AllowedProviders, d.AllowedProviders) || !slices.Equal(s.Requirements, kinds) {
						t.Fatalf("%s %s: summary %+v, decision %+v", officeID, coverage, s, d)
					}
					if d.Participation == "accepted" && len(s.AcceptedAt) != 0 {
						t.Fatalf("%s %s: accepted plan %s lists other offices %v", officeID, coverage, s.PlanID, s.AcceptedAt)
					}
				}
			}
		}
	}
}

func TestListPlansNamesOfficesThatAcceptAMiamiDadeOnlyPlan(t *testing.T) {
	find := func(officeID string) PlanSummary {
		summaries, _ := ListPlans("medical", office(t, officeID))
		for _, s := range summaries {
			if s.PlanID == "aetna-better-health-medicaid-medical" {
				return s
			}
		}
		t.Fatalf("%s: plan missing", officeID)
		return PlanSummary{}
	}
	hollywood := find("hollywood")
	if hollywood.Outcome != "not_accepted" || !slices.Equal(hollywood.AcceptedAt, []string{"Sweetwater"}) {
		t.Fatalf("hollywood = %+v", hollywood)
	}
	if hollywood.CarrierName != "ICARE HEALTH OPTIONS TPA" || hollywood.Note == "" || slices.Contains(hollywood.Names, hollywood.Label) {
		t.Fatalf("hollywood = %+v", hollywood)
	}
	sweetwater := find("sweetwater")
	if sweetwater.Outcome != "accepted" || len(sweetwater.AcceptedAt) != 0 {
		t.Fatalf("sweetwater = %+v", sweetwater)
	}
}

func TestListPlansIsEmptyWhenTheOfficeDoesNotOfferTheCoverage(t *testing.T) {
	summaries, ok := ListPlans("medical", office(t, "north_miami_beach_optical"))
	if !ok {
		t.Fatal("medical rejected")
	}
	b, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Fatalf("plans = %s", b)
	}
}

func TestListPlansRejectsUnknownCoverage(t *testing.T) {
	for _, coverage := range []string{"", "dental", "Medical"} {
		if _, ok := ListPlans(coverage, office(t, "hollywood")); ok {
			t.Fatalf("coverage %q accepted", coverage)
		}
	}
}

func TestListPlansEncodesEveryFieldEvenWhenEmpty(t *testing.T) {
	summaries, _ := ListPlans("medical", office(t, "hollywood"))
	var fields map[string]any
	b, _ := json.Marshal(summaries[0])
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"planId", "label", "names", "carrierCode", "carrierId", "carrierName", "outcome", "allowedProviders", "requirements", "callerNotice", "note", "acceptedAt"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s in %s", key, b)
		}
	}
	if len(fields) != 12 {
		t.Fatalf("unexpected fields in %s", b)
	}
	for _, s := range summaries {
		if s.Names == nil || s.AllowedProviders == nil || s.Requirements == nil || s.AcceptedAt == nil {
			t.Fatalf("nil list in %+v", s)
		}
	}
}
