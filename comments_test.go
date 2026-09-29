package repo

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoCodeComments(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != "." && ignoredByGoTool(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if !strings.HasPrefix(comment.Text, "//go:") {
					t.Errorf("%s: code comments are not allowed; express intent through names and types", fset.Position(comment.Pos()))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func ignoredByGoTool(dir string) bool {
	return strings.HasPrefix(dir, ".") || strings.HasPrefix(dir, "_") || dir == "vendor" || dir == "testdata"
}
