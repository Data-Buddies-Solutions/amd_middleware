package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const domainImportPath = module + "internal/domain"

var identityParsers = map[string]string{
	"PatientID":  "ParsePatientID",
	"Phone":      "ParsePhone",
	"DOB":        "ParseDOB",
	"PersonName": "NewPersonName",
}

func TestIdentityValuesAreBuiltOnlyByTheirParsers(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != "." && (ignoredByGoTool(entry.Name()) || filepath.ToSlash(path) == "internal/domain") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, conversion := range identityConversions(file) {
			t.Errorf("%s: converts to domain.%s directly; build it with domain.%s so the value is validated",
				fset.Position(conversion.pos), conversion.typeName, identityParsers[conversion.typeName])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIdentityConversionsFlagsEveryDirectConversion(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		conversions int
	}{
		{name: "patient ID", src: `import "` + domainImportPath + `"; func f(raw string) { _ = domain.PatientID(raw) }`, conversions: 1},
		{name: "phone DOB and name", src: `import "` + domainImportPath + `"; func f(s string) { _, _, _ = domain.Phone(s), domain.DOB(s), domain.PersonName(s) }`, conversions: 3},
		{name: "renamed import", src: `import d "` + domainImportPath + `"; func f(s string) { _ = d.PatientID(s) }`, conversions: 1},
		{name: "parenthesized type", src: `import "` + domainImportPath + `"; func f(s string) { _ = (domain.Phone)(s) }`, conversions: 1},
		{name: "dot import", src: `import . "` + domainImportPath + `"; func f(s string) { _ = DOB(s) }`, conversions: 1},
		{name: "parser", src: `import "` + domainImportPath + `"; func f(s string) { _, _ = domain.ParsePatientID(s) }`, conversions: 0},
		{name: "constructor", src: `import "` + domainImportPath + `"; func f(s string) { _ = domain.NewPersonName(s) }`, conversions: 0},
		{name: "back to string", src: `import "` + domainImportPath + `"; func f(id domain.PatientID) { _ = string(id) }`, conversions: 0},
		{name: "other package", src: `import other "example.com/other"; func f(s string) { _ = other.PatientID(s) }`, conversions: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "planted.go", "package planted; "+tt.src, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(identityConversions(file)); got != tt.conversions {
				t.Fatalf("identityConversions found %d, want %d", got, tt.conversions)
			}
		})
	}
}

type identityConversion struct {
	pos      token.Pos
	typeName string
}

func identityConversions(file *ast.File) []identityConversion {
	names := map[string]bool{}
	dotImported := false
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != domainImportPath {
			continue
		}
		switch {
		case spec.Name == nil:
			names["domain"] = true
		case spec.Name.Name == ".":
			dotImported = true
		case spec.Name.Name != "_":
			names[spec.Name.Name] = true
		}
	}
	var found []identityConversion
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		fun := ast.Unparen(call.Fun)
		switch fun := fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := fun.X.(*ast.Ident); ok && names[pkg.Name] && identityParsers[fun.Sel.Name] != "" {
				found = append(found, identityConversion{pos: call.Pos(), typeName: fun.Sel.Name})
			}
		case *ast.Ident:
			if dotImported && identityParsers[fun.Name] != "" {
				found = append(found, identityConversion{pos: call.Pos(), typeName: fun.Name})
			}
		}
		return true
	})
	return found
}
