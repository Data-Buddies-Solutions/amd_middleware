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

func TestNoWallClockOutsideMain(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != "." && (ignoredByGoTool(entry.Name()) || filepath.ToSlash(path) == "cmd/api") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, call := range wallClockReads(file) {
			t.Errorf("%s: time.Now() reads the host clock, which is UTC in production; take the now func() time.Time that cmd/api injects and read calendar dates in domain.EasternLocation()", fset.Position(call.Pos()))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func wallClockReads(file *ast.File) []*ast.CallExpr {
	timePackage := timeImportName(file)
	if timePackage == "" {
		return nil
	}
	isTimeCall := func(expr ast.Expr, name string) bool {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return false
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != name {
			return false
		}
		pkg, ok := selector.X.(*ast.Ident)
		return ok && pkg.Name == timePackage && pkg.Obj == nil
	}

	var nowCalls []*ast.CallExpr
	startedBy := map[*ast.CallExpr]*ast.Ident{}
	sinceArguments := map[*ast.Ident]bool{}
	uses := map[*ast.Object][]*ast.Ident{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			if isTimeCall(node, "Now") && len(node.Args) == 0 {
				nowCalls = append(nowCalls, node)
			}
			if isTimeCall(node, "Since") && len(node.Args) == 1 {
				if ident, ok := node.Args[0].(*ast.Ident); ok {
					sinceArguments[ident] = true
				}
			}
		case *ast.AssignStmt:
			if node.Tok != token.DEFINE || len(node.Lhs) != 1 || len(node.Rhs) != 1 || !isTimeCall(node.Rhs[0], "Now") {
				return true
			}
			if ident, ok := node.Lhs[0].(*ast.Ident); ok && ident.Obj != nil {
				startedBy[node.Rhs[0].(*ast.CallExpr)] = ident
			}
		case *ast.Ident:
			if node.Obj != nil {
				uses[node.Obj] = append(uses[node.Obj], node)
			}
		}
		return true
	})

	var reads []*ast.CallExpr
	for _, call := range nowCalls {
		if !timesOnlyElapsed(startedBy[call], uses, sinceArguments) {
			reads = append(reads, call)
		}
	}
	return reads
}

func timesOnlyElapsed(start *ast.Ident, uses map[*ast.Object][]*ast.Ident, sinceArguments map[*ast.Ident]bool) bool {
	if start == nil {
		return false
	}
	for _, use := range uses[start.Obj] {
		if use != start && !sinceArguments[use] {
			return false
		}
	}
	return true
}

func timeImportName(file *ast.File) string {
	for _, spec := range file.Imports {
		if imported, _ := strconv.Unquote(spec.Path.Value); imported != "time" {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return "time"
	}
	return ""
}
