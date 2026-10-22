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

const wallClockMessage = "reads the host clock through time.Now, which is UTC in production; take the now func() time.Time that cmd/api injects"

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
		for _, read := range wallClockReads(file) {
			t.Errorf("%s: %s", fset.Position(read.Pos()), wallClockMessage)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWallClockReadsFlagsEveryReferenceToNow(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		reads int
	}{
		{name: "call", src: `import "time"; func f() time.Time { return time.Now() }`, reads: 1},
		{name: "method value", src: `import "time"; func f() { clock := time.Now; _ = clock() }`, reads: 1},
		{name: "argument", src: `import "time"; func g(func() time.Time) {}; func f() { g(time.Now) }`, reads: 1},
		{name: "renamed import", src: `import clock "time"; func f() { _ = clock.Now }`, reads: 1},
		{name: "dot import", src: `import . "time"; func f() { _ = Now() }`, reads: 1},
		{name: "dot import beside a named import", src: `import ("time"; . "time"); func f() time.Time { return Now() }`, reads: 1},
		{name: "timer", src: `import "time"; func f() time.Duration { start := time.Now(); return time.Since(start) }`, reads: 0},
		{name: "timer read as a time", src: `import "time"; func f() time.Time { start := time.Now(); _ = time.Since(start); return start }`, reads: 1},
		{name: "injected clock", src: `import "time"; func f(now func() time.Time) time.Time { return now() }`, reads: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "planted.go", "package planted; "+tt.src, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(wallClockReads(file)); got != tt.reads {
				t.Fatalf("wallClockReads() found %d reads, want %d", got, tt.reads)
			}
		})
	}
}

func wallClockReads(file *ast.File) []ast.Node {
	timePackages := map[string]bool{}
	for _, spec := range file.Imports {
		if imported, _ := strconv.Unquote(spec.Path.Value); imported != "time" {
			continue
		}
		if spec.Name == nil {
			timePackages["time"] = true
			continue
		}
		if spec.Name.Name == "." {
			return []ast.Node{spec}
		}
		timePackages[spec.Name.Name] = true
	}
	if len(timePackages) == 0 {
		return nil
	}
	isTimeFunc := func(expr ast.Expr, name string) bool {
		selector, ok := expr.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != name {
			return false
		}
		pkg, ok := selector.X.(*ast.Ident)
		return ok && timePackages[pkg.Name] && pkg.Obj == nil
	}
	timerStart := func(expr ast.Expr) (*ast.SelectorExpr, bool) {
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 || !isTimeFunc(call.Fun, "Now") {
			return nil, false
		}
		return call.Fun.(*ast.SelectorExpr), true
	}

	var nowReferences []*ast.SelectorExpr
	startedBy := map[*ast.SelectorExpr]*ast.Ident{}
	sinceArguments := map[*ast.Ident]bool{}
	uses := map[*ast.Object][]*ast.Ident{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			if isTimeFunc(node, "Now") {
				nowReferences = append(nowReferences, node)
			}
		case *ast.CallExpr:
			if isTimeFunc(node.Fun, "Since") && len(node.Args) == 1 {
				if ident, ok := node.Args[0].(*ast.Ident); ok {
					sinceArguments[ident] = true
				}
			}
		case *ast.AssignStmt:
			if node.Tok != token.DEFINE || len(node.Lhs) != 1 || len(node.Rhs) != 1 {
				return true
			}
			now, ok := timerStart(node.Rhs[0])
			if !ok {
				return true
			}
			if ident, ok := node.Lhs[0].(*ast.Ident); ok && ident.Obj != nil {
				startedBy[now] = ident
			}
		case *ast.Ident:
			if node.Obj != nil {
				uses[node.Obj] = append(uses[node.Obj], node)
			}
		}
		return true
	})

	var reads []ast.Node
	for _, now := range nowReferences {
		if !timesOnlyElapsed(startedBy[now], uses, sinceArguments) {
			reads = append(reads, now)
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
