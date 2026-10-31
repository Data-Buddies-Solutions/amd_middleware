package http

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"advancedmd-token-management/internal/insurance"
)

type catalogResponse struct {
	Valid  bool                     `json:"valid"`
	Errors []insurance.CatalogError `json:"errors"`
}

func serveCatalogRoute(t *testing.T, method, target string, body []byte, auth string) *httptest.ResponseRecorder {
	t.Helper()
	router := NewRouter(NewHandlers(nil, nil, nil, nil, testNow), "test-secret", nil)
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func exportedCatalog(t *testing.T) insurance.InsuranceCatalog {
	t.Helper()
	w := serveCatalogRoute(t, http.MethodGet, "/api/insurance/catalog", nil, "Bearer test-secret")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("export status=%d type=%q", w.Code, w.Header().Get("Content-Type"))
	}
	var c insurance.InsuranceCatalog
	decoder := json.NewDecoder(w.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func validateCatalog(t *testing.T, body []byte) (int, catalogResponse) {
	t.Helper()
	w := serveCatalogRoute(t, http.MethodPost, "/api/insurance/catalog/validate", body, "Bearer test-secret")
	var got catalogResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %q", w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("type=%q", w.Header().Get("Content-Type"))
	}
	return w.Code, got
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInsuranceCatalogRoutesRequireTheAPISecret(t *testing.T) {
	body := marshal(t, insurance.RunningCatalog())
	for _, route := range []struct{ method, target string }{
		{http.MethodGet, "/api/insurance/catalog"},
		{http.MethodPost, "/api/insurance/catalog/validate"},
	} {
		for _, auth := range []string{"", "Bearer wrong-secret"} {
			if w := serveCatalogRoute(t, route.method, route.target, body, auth); w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with %q: status=%d body=%s", route.method, route.target, auth, w.Code, w.Body)
			}
		}
	}
}

func TestInsuranceCatalogExportValidatesAndRoundTrips(t *testing.T) {
	w := serveCatalogRoute(t, http.MethodGet, "/api/insurance/catalog", nil, "Bearer test-secret")
	exported := w.Body.Bytes()
	code, got := validateCatalog(t, exported)
	if code != http.StatusOK || !got.Valid || got.Errors != nil {
		t.Fatalf("export did not validate: status=%d %+v", code, got)
	}
	again := serveCatalogRoute(t, http.MethodGet, "/api/insurance/catalog", nil, "Bearer test-secret")
	if !bytes.Equal(again.Body.Bytes(), exported) {
		t.Fatal("two exports differ")
	}
	valid := serveCatalogRoute(t, http.MethodPost, "/api/insurance/catalog/validate", exported, "Bearer test-secret")
	if valid.Body.String() != "{\"valid\":true}\n" {
		t.Fatalf("valid body = %q", valid.Body.String())
	}
}

func TestValidateInsuranceCatalogRejectsUnknownFields(t *testing.T) {
	exported := marshal(t, insurance.RunningCatalog())
	for name, body := range map[string][]byte{
		"top level":  append(bytes.TrimSuffix(exported, []byte("}")), []byte(`,"revision":"r1"}`)...),
		"plan":       bytes.Replace(exported, []byte(`"selfPay":false,`), []byte(`"selfPay":false,"unexpected":true,`), 1),
		"row":        bytes.Replace(exported, []byte(`"notice":`), []byte(`"unexpected":1,"notice":`), 1),
		"invalid":    []byte(`{"carriers":`),
		"wrong type": []byte(`{"carriers":{}}`),
	} {
		t.Run(name, func(t *testing.T) {
			code, got := validateCatalog(t, body)
			if code != http.StatusBadRequest || got.Valid || len(got.Errors) != 1 || got.Errors[0].Rule != "invalid_json" || got.Errors[0].Message == "" {
				t.Fatalf("status=%d %+v", code, got)
			}
		})
	}
}

func TestValidateInsuranceCatalogReturnsEveryError(t *testing.T) {
	c := exportedCatalog(t)
	first := &c.Regions[0].Plans[0]
	first.Family = ""
	first.CarrierCode = ""
	first.CarrierID = "car404"
	code, got := validateCatalog(t, marshal(t, c))
	want := []insurance.CatalogError{
		{Region: "south_florida", PlanID: first.ID, Rule: "invalid_family", Message: `family "" is empty or has surrounding spaces`},
		{Region: "south_florida", PlanID: first.ID, Rule: "unknown_carrier", Message: "carrierId car404 is not in carriers"},
	}
	if code != http.StatusBadRequest || got.Valid || !reflect.DeepEqual(got.Errors, want) {
		t.Fatalf("status=%d errors=%+v", code, got.Errors)
	}
}

func TestValidateInsuranceCatalogNamesEachBrokenRule(t *testing.T) {
	type catalog = insurance.InsuranceCatalog
	southFlorida := func(c *catalog) *insurance.CatalogRegion { return &c.Regions[0] }
	springHill := func(c *catalog) *insurance.CatalogRegion { return &c.Regions[1] }
	doctors := func(c *catalog) *insurance.CatalogTable { return &c.Regions[0].Tables[0] }
	tests := []struct {
		name   string
		want   []string
		change func(t *testing.T, c *catalog)
	}{
		{"carrier listed twice", []string{"duplicate_carrier"}, func(t *testing.T, c *catalog) { c.Carriers = append(c.Carriers, c.Carriers[0]) }},
		{"region listed twice", []string{"duplicate_region"}, func(t *testing.T, c *catalog) { c.Regions = append(c.Regions, c.Regions[0]) }},
		{"table listed twice", []string{"duplicate_table"}, func(t *testing.T, c *catalog) {
			springHill(c).Tables = append(springHill(c).Tables, springHill(c).Tables[0])
		}},
		{"table without offices", []string{"table_without_offices"}, func(t *testing.T, c *catalog) {
			tables := springHill(c).Tables
			tables[1].Offices = append(tables[1].Offices, tables[0].Offices...)
			tables[0].Offices = []string{}
		}},
		{"unknown office", []string{"unknown_office"}, func(t *testing.T, c *catalog) {
			doctors(c).Offices = append(doctors(c).Offices, "atlantis")
		}},
		{"office in two tables", []string{"office_in_two_tables"}, func(t *testing.T, c *catalog) {
			tables := springHill(c).Tables
			tables[1].Offices = append(tables[1].Offices, tables[0].Offices...)
		}},
		{"office without a table, leaving its own plans in none", []string{"office_without_table", "plan_in_no_table"}, func(t *testing.T, c *catalog) {
			springHill(c).Tables = springHill(c).Tables[1:]
		}},
		{"unknown doctor column", []string{"unknown_doctor_column"}, func(t *testing.T, c *catalog) {
			doctors(c).Doctors = append(doctors(c).Doctors, "Dr. Nobody")
		}},
		{"row names an unknown doctor", []string{"unknown_doctor_column"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows[0].Doctors["Dr. Nobody"] = "yes"
		}},
		{"missing doctor column", []string{"missing_doctor_column"}, func(t *testing.T, c *catalog) {
			table := doctors(c)
			last := len(table.Doctors) - 1
			removed := table.Doctors[last]
			table.Doctors = table.Doctors[:last]
			for _, row := range table.Rows {
				delete(row.Doctors, removed)
			}
		}},
		{"doctor column listed twice", []string{"duplicate_doctor_column"}, func(t *testing.T, c *catalog) {
			doctors(c).Doctors = append(doctors(c).Doctors, doctors(c).Doctors[0])
		}},
		{"plan id not kebab case", []string{"invalid_plan_id"}, func(t *testing.T, c *catalog) {
			renamePlan(southFlorida(c), southFlorida(c).Plans[0].ID, "Aetna_Medicare")
		}},
		{"plan listed twice", []string{"duplicate_plan"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans = append(southFlorida(c).Plans, southFlorida(c).Plans[0])
		}},
		{"plan id reused for a different plan", []string{"plan_id_reused"}, func(t *testing.T, c *catalog) {
			moved := springHill(c).Plans[0]
			for _, p := range southFlorida(c).Plans {
				if p.CarrierID != moved.CarrierID && !slices.ContainsFunc(springHill(c).Plans, func(q insurance.CatalogPlan) bool { return q.ID == p.ID }) {
					renamePlan(springHill(c), moved.ID, p.ID)
					return
				}
			}
			t.Fatal("no south Florida plan to reuse")
		}},
		{"empty family", []string{"invalid_family"}, func(t *testing.T, c *catalog) { southFlorida(c).Plans[0].Family = "" }},
		{"padded family", []string{"invalid_family"}, func(t *testing.T, c *catalog) { southFlorida(c).Plans[0].Family = "Aetna " }},
		{"unknown coverage", []string{"unknown_coverage"}, func(t *testing.T, c *catalog) { southFlorida(c).Plans[0].Coverage = "dental" }},
		{"name of filler words", []string{"filler_only_name"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans[0].Aliases = append(southFlorida(c).Plans[0].Aliases, "insurance plan")
		}},
		{"name repeated", []string{"repeated_name"}, func(t *testing.T, c *catalog) {
			p := &southFlorida(c).Plans[0]
			p.Aliases = append(p.Aliases, strings.ToUpper(p.Aliases[0]))
		}},
		{"name shared by two plans", []string{"name_conflict"}, func(t *testing.T, c *catalog) {
			plans := southFlorida(c).Plans
			plans[1].Aliases = append(plans[1].Aliases, plans[0].Label)
		}},
		{"unknown carrier", []string{"unknown_carrier"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans[0].CarrierCode = ""
			southFlorida(c).Plans[0].CarrierID = "car404"
		}},
		{"carrier code with two carriers", []string{"carrier_code_conflict"}, func(t *testing.T, c *catalog) {
			plans := southFlorida(c).Plans
			plans[1].CarrierCode = plans[0].CarrierCode
		}},
		{"plan in no table", []string{"plan_in_no_table"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans = append(southFlorida(c).Plans, insurance.CatalogPlan{ID: "ghost-plan", Label: "Ghost Plan", Family: "Ghost", Coverage: "medical", Aliases: []string{}})
		}},
		{"row for an unknown plan", []string{"unknown_plan"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows = append(doctors(c).Rows, insurance.CatalogRow{PlanID: "ghost-plan"})
		}},
		{"plan with two rows", []string{"duplicate_row"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows = append(doctors(c).Rows, doctors(c).Rows[0])
		}},
		{"unknown doctor value", []string{"invalid_doctor_value"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows[0].Doctors[doctors(c).Doctors[0]] = "maybe"
		}},
		{"accepted plan without a carrier", []string{"accepted_plan_without_carrier"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans[0].CarrierCode = ""
			southFlorida(c).Plans[0].CarrierID = ""
		}},
		{"only office outside the table", []string{"only_office_outside_table"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows[0].OnlyOffices = []string{"spring_hill"}
		}},
		{"unknown requirement", []string{"unknown_requirement"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows[0].Requires = []insurance.CatalogRequirement{{Kind: "vob_authorization"}}
		}},
		{"two self-pay plans for one coverage", []string{"two_self_pay_plans"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans[0].SelfPay = true
		}},
		{"note states an unencoded rule", []string{"note_rule_not_encoded"}, func(t *testing.T, c *catalog) {
			doctors(c).Rows[0].Note = "Requires referral from the PCP"
		}},
		{"name decides another plan", []string{"name_does_not_resolve"}, func(t *testing.T, c *catalog) {
			southFlorida(c).Plans[0].Aliases = append(southFlorida(c).Plans[0].Aliases, "Medicade")
		}},
		{"office without an accepted self-pay plan", []string{"no_accepted_self_pay"}, func(t *testing.T, c *catalog) {
			for i := range southFlorida(c).Plans {
				southFlorida(c).Plans[i].SelfPay = false
			}
		}},
		{"known phrasing decides the plan it contradicts", []string{"contradicted_phrasing"}, func(t *testing.T, c *catalog) {
			plan := findPlan(t, southFlorida(c), "staywell-medicare-medical")
			plan.Aliases = append(plan.Aliases, "Staywell Medicaid")
		}},
	}
	covered := []string{}
	for _, tc := range tests {
		covered = append(covered, tc.want...)
		t.Run(tc.name, func(t *testing.T) {
			c := exportedCatalog(t)
			tc.change(t, &c)
			code, got := validateCatalog(t, marshal(t, c))
			rules := []string{}
			for _, e := range got.Errors {
				if e.Message == "" {
					t.Errorf("%s error has no message", e.Rule)
				}
				rules = append(rules, string(e.Rule))
			}
			slices.Sort(rules)
			if code != http.StatusBadRequest || got.Valid || !slices.Equal(slices.Compact(rules), tc.want) {
				t.Fatalf("status=%d valid=%v errors=%+v, want rules %v", code, got.Valid, got.Errors, tc.want)
			}
		})
	}
	slices.Sort(covered)
	if rules := catalogRules(t); !slices.Equal(slices.Compact(covered), rules) {
		t.Fatalf("cases cover rules %v, validator defines %v; add one invalid catalog per rule", slices.Compact(covered), rules)
	}
}

func renamePlan(region *insurance.CatalogRegion, from, to string) {
	for i := range region.Plans {
		if region.Plans[i].ID == from {
			region.Plans[i].ID = to
		}
	}
	for _, table := range region.Tables {
		for i := range table.Rows {
			if table.Rows[i].PlanID == from {
				table.Rows[i].PlanID = to
			}
		}
	}
}

func findPlan(t *testing.T, region *insurance.CatalogRegion, id string) *insurance.CatalogPlan {
	t.Helper()
	for i := range region.Plans {
		if region.Plans[i].ID == id {
			return &region.Plans[i]
		}
	}
	t.Fatalf("%s has no plan %s", region.ID, id)
	return nil
}

func catalogRules(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../insurance/validate.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	rules := []string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, ok := spec.Type.(*ast.Ident); !ok || ident.Name != "CatalogRule" {
			return true
		}
		for _, value := range spec.Values {
			literal, _ := strconv.Unquote(value.(*ast.BasicLit).Value)
			rules = append(rules, literal)
		}
		return true
	})
	slices.Sort(rules)
	if len(rules) == 0 {
		t.Fatal("found no CatalogRule constants in ../insurance/validate.go")
	}
	return rules
}
