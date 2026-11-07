package http

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"advancedmd-token-management/internal/insurance"
	patientmodule "advancedmd-token-management/internal/patient"
	schedulingmodule "advancedmd-token-management/internal/scheduling"
)

type contractRoute struct {
	Request []string `json:"request,omitempty"`
	Reply   []string `json:"reply"`
}

type contractFixture struct {
	Routes   map[string]contractRoute `json:"routes"`
	Outcomes map[string][]string      `json:"outcomes"`
}

func jsonFields(t reflect.Type, prefix string, fields map[string]bool, walking map[reflect.Type]bool) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		jsonFields(t.Elem(), prefix, fields, walking)
		return
	case reflect.Map:
		jsonFields(t.Elem(), prefix+"{}.", fields, walking)
		return
	case reflect.Struct:
	default:
		return
	}
	if walking[t] {
		return
	}
	walking[t] = true
	defer delete(walking, t)
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[prefix+name] = true
		jsonFields(field.Type, prefix+name+".", fields, walking)
	}
}

func fieldsOf(v any) []string {
	fields := map[string]bool{}
	jsonFields(reflect.TypeOf(v), "", fields, map[reflect.Type]bool{})
	return slices.Sorted(maps.Keys(fields))
}

func constantValues(t *testing.T, packageDir string, belongs func(name, typeName string) bool) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(packageDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				typeName := ""
				if ident, ok := value.Type.(*ast.Ident); ok {
					typeName = ident.Name
				}
				for i, name := range value.Names {
					if i >= len(value.Values) || !belongs(name.Name, typeName) {
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Fatalf("%s must be a string literal to enter the contract", name.Name)
					}
					text, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					values[text] = true
				}
			}
		}
	}
	found := slices.Sorted(maps.Keys(values))
	if len(found) == 0 {
		t.Fatalf("no constants found in %s", packageDir)
	}
	return found
}

func namedPrefix(prefix string) func(name, typeName string) bool {
	return func(name, _ string) bool { return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) }
}

func typed(want string) func(name, typeName string) bool {
	return func(_, typeName string) bool { return typeName == want }
}

func currentContract(t *testing.T) contractFixture {
	t.Helper()
	const scheduling, patient = "../scheduling", "../patient"
	return contractFixture{
		Routes: map[string]contractRoute{
			"POST /api/patient/resolve":          {Request: fieldsOf(patientmodule.ResolveCommand{}), Reply: fieldsOf(patientmodule.ResolveResult{})},
			"POST /api/add-patient":              {Request: fieldsOf(patientmodule.CreateCommand{}), Reply: fieldsOf(patientmodule.CreateResult{})},
			"POST /api/patient/update-insurance": {Request: fieldsOf(patientmodule.UpdateInsuranceCommand{}), Reply: fieldsOf(patientmodule.UpdateInsuranceResult{})},
			"GET /api/insurance/plans":           {Reply: fieldsOf(insurance.PlanList{})},
			"POST /api/scheduler/slots":          {Request: fieldsOf(schedulingmodule.ListCommand{}), Reply: fieldsOf(schedulingmodule.AvailabilityResponse{})},
			"POST /api/appointment/book":         {Request: fieldsOf(schedulingmodule.BookCommand{}), Reply: fieldsOf(schedulingmodule.BookReceipt{})},
			"POST /api/appointment/reschedule":   {Request: fieldsOf(schedulingmodule.BookCommand{}), Reply: fieldsOf(schedulingmodule.RescheduleReceipt{})},
			"POST /api/appointment/cancel":       {Request: fieldsOf(schedulingmodule.CancelCommand{}), Reply: fieldsOf(schedulingmodule.CancelReceipt{})},
		},
		Outcomes: map[string][]string{
			"scheduling":        constantValues(t, scheduling, typed("Category")),
			"availability":      constantValues(t, scheduling, namedPrefix("AvailabilityOutcome")),
			"availabilityNext":  constantValues(t, scheduling, namedPrefix("AvailabilityNextAction")),
			"patientMutation":   constantValues(t, patient, typed("MutationOutcome")),
			"patientResolution": constantValues(t, patient, typed("Status")),
			"appointmentsRead":  constantValues(t, patient, typed("AppointmentsStatus")),
			"patientCreate":     constantValues(t, patient, typed("CreateStatus")),
			"insuranceUpdate":   constantValues(t, patient, typed("UpdateInsuranceStatus")),
		},
	}
}

func TestContractFixtureMatchesTheWireTypes(t *testing.T) {
	path := filepath.Join("testdata", "s2s_contract.json")
	got, err := json.MarshalIndent(currentContract(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/http -run ContractFixture -update)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("the s2s wire contract changed; if intended, run go test ./internal/http -run ContractFixture -update, review the diff of %s, and update s2s in the order the change requires", path)
	}
}
