package insurance

import (
	"encoding/json"
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
		{"alias repeats the label", "repeated", func(sf, _, _ *[]plan) { (*sf)[0].Names = []string{"Aetna Better Health"} }},
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

func TestParseCatalogRequiresEveryOfficeInExactlyOneTable(t *testing.T) {
	withTables := func(change func(tables map[string][]string)) map[string][]byte {
		files := syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
		tables := map[string][]string{}
		if err := json.Unmarshal(files[officeTablesFile], &tables); err != nil {
			t.Fatal(err)
		}
		change(tables)
		b, err := json.Marshal(tables)
		if err != nil {
			t.Fatal(err)
		}
		files[officeTablesFile] = b
		return files
	}
	tests := []struct {
		name  string
		files map[string][]byte
		want  string
	}{
		{"office without a table", withTables(func(tables map[string][]string) {
			delete(tables, "crystal_river/doctors.csv")
		}), "is not used by"},
		{"office in two tables", withTables(func(tables map[string][]string) {
			tables["crystal_river/doctors.csv"] = []string{"hollywood"}
		}), `office "hollywood" is in`},
		{"unknown office", withTables(func(tables map[string][]string) {
			tables["crystal_river/doctors.csv"] = []string{"atlantis"}
		}), "unknown office"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseCatalog(tc.files); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	files := withTables(func(tables map[string][]string) { delete(tables, "crystal_river/doctors.csv") })
	delete(files, "crystal_river/doctors.csv")
	delete(files, "crystal_river/plans.csv")
	if _, err := parseCatalog(files); err == nil || !strings.Contains(err.Error(), `"crystal_river" has no plan list`) {
		t.Fatalf("missing office err = %v", err)
	}
}

func TestParseCatalogRejectsMalformedFiles(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		change func(files map[string][]byte)
	}{
		{"stray file", "is not used by", func(files map[string][]byte) { files["notes/extra.csv"] = []byte("plan\n") }},
		{"unknown doctor column", "unknown doctor", func(files map[string][]byte) {
			files["south_florida/doctors.csv"] = []byte("plan,Dr. Nobody,requires,only_offices,notice,note\n")
		}},
		{"missing table column", "header must be", func(files map[string][]byte) {
			files["south_florida/doctors.csv"] = []byte("plan,Dr. Austin Bach,requires,notice,note\n")
		}},
		{"plan columns", "header must be", func(files map[string][]byte) {
			files["south_florida/plans.csv"] = []byte("id,label\n")
		}},
		{"row for unknown plan", "is not in plans.csv", func(files map[string][]byte) {
			table := string(files["south_florida/doctors.csv"])
			header, _, _ := strings.Cut(table, "\n")
			files["south_florida/doctors.csv"] = []byte(table + "ghost-plan" + strings.Repeat(",", strings.Count(header, ",")) + "\n")
		}},
		{"plan without a row", "is in no office table", func(files map[string][]byte) {
			files["south_florida/plans.csv"] = append(files["south_florida/plans.csv"], []byte("ghost-plan,Ghost,medical,,car1,,\n")...)
		}},
		{"bad self pay", "self_pay", func(files map[string][]byte) {
			files["south_florida/plans.csv"] = []byte(strings.Replace(string(files["south_florida/plans.csv"]), ",car301672,yes,", ",car301672,maybe,", 1))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := syntheticFiles(t, syntheticSouthFlorida(), nil, nil)
			tc.change(files)
			if _, err := parseCatalog(files); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
