package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavePrivateAndExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "result.json")
	if err := save(p, map[string]string{"status": "review"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal("result must be private")
	}
	if save(p, nil) == nil {
		t.Fatal("must not overwrite evidence")
	}
}
