package repo

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "advancedmd-token-management/"

var packageLayers = map[string]string{
	"internal/domain":                    "value",
	"internal/safeerrors":                "value",
	"internal/safelog":                   "value",
	"internal/session":                   "session",
	"internal/clients":                   "transport",
	"internal/advancedmd":                "records",
	"internal/advancedmd/advancedmdtest": "records",
	"internal/insurance":                 "policy",
	"internal/patient":                   "feature",
	"internal/scheduling":                "feature",
	"internal/http":                      "handler",
	"internal/config":                    "composition",
	"cmd/api":                            "composition",
}

var allowedLayerImports = map[string][]string{
	"value":       {},
	"session":     {"value"},
	"transport":   {"value", "session"},
	"records":     {"value", "session", "transport", "records"},
	"policy":      {"value"},
	"feature":     {"value", "records", "policy"},
	"handler":     {"value", "session", "records", "policy", "feature"},
	"composition": {"value", "session", "transport", "records", "policy", "feature", "handler", "composition"},
}

func TestPackagesImportOnlyLowerLayers(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != "." && ignoredByGoTool(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Dir(path) == "." {
			return nil
		}
		pkg := filepath.ToSlash(filepath.Dir(path))
		layer, ok := packageLayers[pkg]
		if !ok {
			t.Errorf("%s: package %s has no layer in packageLayers", path, pkg)
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			target, internal := strings.CutPrefix(imported, module)
			if !internal || target == pkg {
				continue
			}
			if !slices.Contains(allowedLayerImports[layer], packageLayers[target]) {
				t.Errorf("%s: %s layer %s must not import %s layer %s", path, layer, pkg, packageLayers[target], target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
