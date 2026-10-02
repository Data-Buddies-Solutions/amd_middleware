package insurance

import (
	"strings"
	"testing"
)

func TestParseCatalogRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		change func(sf, sh, cr *[]plan)
	}{
		{"id not kebab", "kebab", func(sf, _, _ *[]plan) { (*sf)[0].ID = "Aetna_Better" }},
		{"duplicate id", "duplicate plan id", func(sf, _, _ *[]plan) { (*sf)[1].ID = (*sf)[0].ID }},
		{"id reused for another carrier", "reused", func(sf, sh, _ *[]plan) {
			*sh = append(*sh, plan{ID: "aetna-medicare", Label: "Other", Coverage: "medical", CarrierID: "car1"})
		}},
		{"duplicate label", "belongs to", func(sf, _, _ *[]plan) { (*sf)[1].Label = "Aetna  better health" }},
		{"duplicate name", "belongs to", func(sf, _, _ *[]plan) { (*sf)[1].Names = []string{"Aetna Better Health MMA"} }},
		{"empty name", "only filler", func(sf, _, _ *[]plan) { (*sf)[0].Names = []string{"insurance plan"} }},
		{"repeated name", "repeated", func(sf, _, _ *[]plan) { (*sf)[0].Names = []string{"Aetna MMA", "aetna mma"} }},
		{"unknown coverage", "coverage", func(sf, _, _ *[]plan) { (*sf)[0].Coverage = "dental" }},
		{"unknown doctor", "unknown doctor", func(sf, _, _ *[]plan) { (*sf)[0].Doctors = map[string]string{"Dr. Bach": "yes"} }},
		{"unknown doctor value", "has value", func(sf, _, _ *[]plan) { (*sf)[0].Doctors = bach("maybe") }},
		{"yes without carrier", "no carrierId", func(sf, _, _ *[]plan) { (*sf)[0].CarrierID = "" }},
		{"unknown carrier", "carriers.json", func(sf, _, _ *[]plan) { (*sf)[0].CarrierID = "car404" }},
		{"carrier code with two carriers", "carrier code", func(sf, _, _ *[]plan) { (*sf)[1].CarrierID = "car1" }},
		{"only office outside list", "onlyOffices", func(sf, _, _ *[]plan) { (*sf)[0].OnlyOffices = []string{"spring_hill"} }},
		{"unknown requirement", "requirement", func(sf, _, _ *[]plan) {
			(*sf)[0].Requirements = []planRequirement{{Kind: "vob_authorization"}}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sf, sh, cr := syntheticSouthFlorida(), springHillPlans(), []plan{}
			tc.change(&sf, &sh, &cr)
			_, err := parseCatalog(syntheticFiles(t, sf, sh, cr))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseCatalogRequiresEveryOfficeInExactlyOneList(t *testing.T) {
	files := syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
	delete(files, "crystal_river.json")
	if _, err := parseCatalog(files); err == nil || !strings.Contains(err.Error(), `"crystal_river" has no plan list`) {
		t.Fatalf("missing office err = %v", err)
	}
	files = syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
	files["extra.json"] = []byte(`{"offices":["hollywood"],"plans":[]}`)
	if _, err := parseCatalog(files); err == nil || !strings.Contains(err.Error(), `office "hollywood" is in`) {
		t.Fatalf("duplicate office err = %v", err)
	}
	files = syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
	files["extra.json"] = []byte(`{"offices":["atlantis"],"plans":[]}`)
	if _, err := parseCatalog(files); err == nil || !strings.Contains(err.Error(), "unknown office") {
		t.Fatalf("unknown office err = %v", err)
	}
	files = syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
	files["extra.json"] = []byte(`{"offices":["hollywood"],"plans":[],"typo":true}`)
	if _, err := parseCatalog(files); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field err = %v", err)
	}
}
