package eligibility

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestEveryOfficeInsuranceHasExplicitPayerDisposition(t *testing.T) {
	raw, err := os.ReadFile("../domain/insurance_data/MEDICAL.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Plans []struct {
			Name string `json:"name"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, plan := range catalog.Plans {
		seen[domain.NormalizeForLookup(plan.Name)] = true
	}
	for plan := range domain.VisionInsuranceNameMap {
		seen[plan] = true
	}
	if len(catalog.Plans) == 0 {
		t.Fatal("medical catalog was not inspected")
	}
	for plan := range seen {
		if _, ok := insurancePayers[plan]; !ok {
			t.Errorf("insurance %q has no explicit payer disposition", plan)
		}
	}
	t.Logf("%d distinct office insurance products have explicit payer dispositions", len(seen))
}

func TestPayerRoutesMatchVerifiedStediDirectory(t *testing.T) {
	file, err := os.Open("testdata/stedi-payers-20260916.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	directory := map[string][]string{}
	for _, row := range rows[1:] {
		directory[row[1]] = row
	}
	for plan, route := range insurancePayers {
		if route.payer == "" {
			if route.review == "" {
				t.Errorf("%s has neither payer nor review", plan)
			}
			continue
		}
		row, ok := directory[route.payer]
		if !ok {
			t.Errorf("%s: payer %s has no verified source", plan, route.payer)
			continue
		}
		if route.review == "" && (row[3] != "true" || row[4] != "false") {
			t.Errorf("%s dispatches unsupported/unenrolled payer", plan)
		}
		if row[3] == "false" && route.review != "payer_eligibility_not_supported" {
			t.Errorf("%s does not expose unsupported eligibility", plan)
		}
	}
}

func TestProductRoutingSeparatesSharedCarriersAndAliases(t *testing.T) {
	for _, tc := range []struct{ plan, payer string }{
		{"Aetna", "60054"}, {"Aetna Better Health of Florida", "128FL"},
		{"Aetna Healthy Kids", "128FL"}, {"Aetna Medicare HMO", "60054"},
		{"Simply Healthcare", "SMPLY"}, {"Simply Medicare (Medical)", "SMPLY"},
		{"Humana Medicare", "61101"}, {"Molina Medicare", "51062"},
		{"UHC", "87726"}, {"United Health Care", "87726"}, {"Ambetter from Sunshine Health", "68069"}, {"UHC Dual Complete", "87726"},
		{"United Healthcare All Savers", "81400"}, {"United Healthcare Golden Rule", "37602"},
		{"United Healthcare Shared Services", "39026"}, {"United Healthcare Surest", "25463"},
		{"United Healthcare Student Resources", "74227"}, {"United Healthcare Oxford", "06111"},
		{"UHC Community Plan", "04567"},
		{"Florida Blue Select", "BCBSF"}, {"FL Blue", "BCBSF"}, {"Florida Blue", "BCBSF"}, {"Florida Blue Medicare PPO", "FBM01"},
		{"Cigna PPO", "62308"}, {"Cigna Medicare Advantage", "63092"},
		{"TRICARE Prime", "99727"}, {"TRICARE for Life", "TDFIC"},
		{"Devoted Medicare HMO", "DEVOT"}, {"Solis Medicare", "SOLIS"},
		{"Preferred Care Network", "78857"}, {"Preferred Care Partners", "65088"},
		{"Sunshine Health", "68069"}, {"Ambetter", "68069"}, {"Envolve Vision", "46278"},
		{"Davis Vision", "00157"}, {"Spectera Vision", "00773"}, {"Oscar Insurance", "OSCAR"},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			payer, review := Route(tc.plan, "20260916")
			if payer != tc.payer || review != "" {
				t.Fatalf("got %s/%s, want %s", payer, review, tc.payer)
			}
		})
	}
	for _, plan := range []string{"Blue Cross", "BCBS", "BCBS Medicare HMO", "Preferred Care", "Wellcare Medicaid", "Metlife", "Versant", "Ambetter Vision", "Preferred Care Network Preferred Care Partners (Medical)", "TRICARE", "United Health One", "Medicaid", "Staywell", "Vivida", "Imagine Health", "Partners Direct Health", "car40907", "aetnaa"} {
		if _, review := Route(plan, "20260916"); review == "" {
			t.Errorf("ambiguous or unknown %s selected a payer", plan)
		}
	}
	payer, review := Route("iCare", "20260916")
	if payer != "26054" || review != "payer_eligibility_not_supported" {
		t.Fatal("iCare TPA was confused with the unrelated iCare insurer")
	}
}

func TestCMSPlanUsesServiceDateAndIsNotOriginalMedicare(t *testing.T) {
	for _, tc := range []struct{ date, payer string }{{"20260930", "68069"}, {"20261001", "51062"}, {"20261201", "51062"}} {
		payer, review := Route("Children's Medical Services", tc.date)
		if payer != tc.payer || review != "" {
			t.Fatalf("CMS plan %s got %s/%s", tc.date, payer, review)
		}
	}
	for _, date := range []string{"20260230", "", "20210101"} {
		if _, review := Route("childrens medical services", date); review == "" {
			t.Fatal("invalid/unsupported date accepted")
		}
	}
	if payer, review := Route("Original Medicare", "20261001"); payer != "CMS" || review == "" {
		t.Fatal("Original Medicare must retain its enrollment gate")
	}
}

func TestMappedRoutesSendOnlySTC30AndBlockedRoutesDoNotSend(t *testing.T) {
	calls := 0
	var actualPayer string
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Payer != actualPayer {
			t.Errorf("wire payer=%s want=%s", request.Payer, actualPayer)
		}
		if len(request.Encounter.ServiceTypeCodes) != 1 || request.Encounter.ServiceTypeCodes[0] != "30" {
			t.Error("non-STC30 request")
		}
		fmt.Fprint(w, fixture(`[{"code":"1","serviceTypeCodes":["30"]}]`, ""))
	})
	for plan, route := range insurancePayers {
		in := input()
		in.Plan = plan
		actualPayer = route.payer
		before := calls
		out, err := s.Check(context.Background(), "office", in)
		if err != nil {
			t.Fatal(err)
		}
		if out.PayerID != route.payer {
			t.Errorf("%s receipt omits resolved payer", plan)
		}
		if route.review != "" {
			if calls != before || out.ReviewReason != route.review {
				t.Errorf("blocked route %s sent or lost its review reason", plan)
			}
		} else if calls != before+1 || out.Status != "active" {
			t.Errorf("supported route %s did not execute", plan)
		}
	}
}

func TestAmbiguousAndBillingAliasesNeverDispatch(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) { t.Error("ambiguous alias reached Stedi") })
	for _, plan := range []string{"Preferred Care", "BCBS Medicare HMO", "Wellcare Medicaid", "Metlife", "Versant", "Ambetter Vision", "AvMed Entrust", "Leon Health Plan", "Seminole Tribe", "Aetna and Cigna"} {
		in := input()
		in.Plan = plan
		out, err := s.Check(context.Background(), "office", in)
		if err != nil || out.Status != "review" || out.ReviewReason == "" {
			t.Errorf("%s: %+v %v", plan, out, err)
		}
	}
}
