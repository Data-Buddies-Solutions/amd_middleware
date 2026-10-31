package insurance

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"advancedmd-token-management/internal/domain"
)

func TestV2FixtureIsTheConvertedV1Fixture(t *testing.T) {
	v1, err := os.ReadFile("testdata/catalog_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	converted, err := DecodeCatalog(v1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(converted)
	if err != nil {
		t.Fatal(err)
	}
	if *updateGolden {
		if err := os.WriteFile("testdata/catalog.json", got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("testdata/catalog.json is not testdata/catalog_v1.json converted; run go test ./internal/insurance -run V2FixtureIsTheConvertedV1Fixture -update")
	}
}

func TestDecodeCatalogConvertsV1ByTheContractRules(t *testing.T) {
	v1 := `{"carriers":[{"id":"car1","name":"ONE"},{"id":"car2","name":"TWO"},{"id":"car3","name":"THREE"}],"regions":[
		{"id":"a","plans":[
			{"id":"p1","label":"P1","family":"F","coverage":"medical","carrierCode":"BBB","carrierId":"car1","selfPay":false,"aliases":[]},
			{"id":"p2","label":"P2","family":"F","coverage":"medical","carrierCode":"","carrierId":"car1","selfPay":false,"aliases":[]},
			{"id":"p3","label":"P3","family":"F","coverage":"medical","carrierCode":"AAA","carrierId":"car2","selfPay":false,"aliases":[]}
		],"tables":[{"id":"t","offices":["hollywood"],"doctors":["Dr. Austin Bach"],"rows":[
			{"planId":"p1","doctors":{},"requires":[],"onlyOffices":[],"notice":"Bring your card.","note":"anything"},
			{"planId":"p2","doctors":{},"requires":[{"kind":"pcp_referral","channel":""},{"kind":"staff_verify","channel":""}],"onlyOffices":[],"notice":"","note":""},
			{"planId":"p3","doctors":{},"requires":[{"kind":"prior_authorization","channel":"availity"},{"kind":"pcp_referral","channel":""}],"onlyOffices":[],"notice":"","note":""}
		]}]},
		{"id":"b","plans":[
			{"id":"p4","label":"P4","family":"F","coverage":"medical","carrierCode":"AAA","carrierId":"car1","selfPay":false,"aliases":[]},
			{"id":"p5","label":"P5","family":"F","coverage":"medical","carrierCode":"ZZZ","carrierId":"car2","selfPay":true,"aliases":[]},
			{"id":"p6","label":"P6","family":"F","coverage":"medical","carrierCode":"BBB","carrierId":"car1","selfPay":false,"aliases":[]}
		],"tables":[]}
	]}`
	got, err := DecodeCatalog([]byte(v1))
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, c := range got.Carriers {
		codes[c.ID] = c.Code
	}
	if want := map[string]string{"car1": "BBB", "car2": "AAA", "car3": ""}; !reflect.DeepEqual(codes, want) {
		t.Errorf("carrier codes = %v, want %v (most common, ties lexical, none empty)", codes, want)
	}
	rows := got.Regions[0].Tables[0].Rows
	before := []BeforeBooking{rows[0].BeforeBooking, rows[1].BeforeBooking, rows[2].BeforeBooking}
	if want := []BeforeBooking{BeforeBookingNone, BeforeBookingStaffVerify, BeforeBookingPriorAuthorization}; !slices.Equal(before, want) {
		t.Errorf("beforeBooking = %v, want %v", before, want)
	}
	if rows[0].Notice != "Bring your card." {
		t.Errorf("notice = %q", rows[0].Notice)
	}
}

func TestDecodeCatalogRejectsAMixOfV1AndV2(t *testing.T) {
	b, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	mixed := bytes.Replace(b, []byte(`"beforeBooking":`), []byte(`"note":"","beforeBooking":`), 1)
	if _, err := DecodeCatalog(mixed); err == nil || !strings.Contains(err.Error(), "not a v2 catalog") || !strings.Contains(err.Error(), "not a v1 catalog") {
		t.Fatalf("err = %v", err)
	}
}

func TestV1CatalogDecidesAsItDidBeforeV2(t *testing.T) {
	v1, err := os.ReadFile("testdata/catalog_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	previous := active.Load()
	t.Cleanup(func() { active.Store(previous) })
	if err := Refresh(context.Background(), func(context.Context) (string, []byte, error) { return "v1", v1, nil }); err != nil {
		t.Fatal(err)
	}
	want := recordedV1Decisions(t, carrierCodes(t))
	got := map[string]any{}
	forEachOfficeCoverage(t, func(o *domain.OfficeConfig, coverage string) {
		list, _ := ListPlans(coverage, o)
		for _, summary := range list.Plans {
			got[o.ID+"/"+coverage+"/"+summary.PlanID] = asJSON(t, map[string]any{
				"summary": summary,
				"adult":   DecidePlan(summary.PlanID, coverage, o, adultDOB, testNow),
				"child":   DecidePlan(summary.PlanID, coverage, o, dobYearsAgo(8), testNow),
			})
		}
	})
	if len(got) != len(want) {
		t.Fatalf("decided %d plans at office coverages, recorded %d", len(got), len(want))
	}
	for key, recorded := range want {
		if !reflect.DeepEqual(got[key], recorded) {
			t.Errorf("%s decides differently from v1\n got %v\nwant %v", key, got[key], recorded)
		}
	}
}

func carrierCodes(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := DecodeCatalog(b)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, carrier := range catalog.Carriers {
		codes[carrier.ID] = carrier.Code
	}
	return codes
}

func recordedV1Decisions(t *testing.T, carrierCodes map[string]string) map[string]any {
	t.Helper()
	f, err := os.Open("testdata/catalog_v1_decisions.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	unzipped, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	recorded := map[string]any{}
	lines := bufio.NewScanner(unzipped)
	lines.Buffer(nil, 1<<20)
	for lines.Scan() {
		var line struct {
			Office   string `json:"office"`
			Coverage string `json:"coverage"`
			Plans    struct {
				Plans []map[string]any `json:"plans"`
			} `json:"plans"`
			Decisions map[string][2]map[string]any `json:"decisions"`
		}
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		for _, summary := range line.Plans.Plans {
			delete(summary, "note")
			summary["requirements"] = v2Requirements(summary["requirements"].([]any))
			id := summary["planId"].(string)
			adult, child := line.Decisions[id][0], line.Decisions[id][1]
			for _, v := range []map[string]any{summary, adult, child} {
				code, _ := v["carrierCode"].(string)
				id, _ := v["carrierId"].(string)
				if code == "" && carrierCodes[id] != "" {
					v["carrierCode"] = carrierCodes[id]
				}
			}
			adult["requirements"] = v2RequirementObjects(adult)
			child["requirements"] = v2RequirementObjects(child)
			recorded[line.Office+"/"+line.Coverage+"/"+id] = asJSON(t, map[string]any{"summary": summary, "adult": adult, "child": child})
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	return recorded
}

func v2Requirements(kinds []any) []any {
	if len(kinds) > 1 && slices.Contains(kinds, any("staff_verify")) {
		return []any{"staff_verify"}
	}
	return kinds
}

func v2RequirementObjects(d map[string]any) []any {
	requirements := d["requirements"].([]any)
	kinds := []any{}
	for _, r := range requirements {
		delete(r.(map[string]any), "channel")
		kinds = append(kinds, r.(map[string]any)["kind"])
	}
	if len(v2Requirements(kinds)) == len(kinds) {
		return requirements
	}
	if d["reason"] == "requirement" {
		d["answer"] = strings.Replace(d["answer"].(string), answerReferral, answerStaffVerify, 1)
	}
	return []any{map[string]any{"kind": "staff_verify", "verification": "unverified"}}
}

func asJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
