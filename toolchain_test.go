package repo

import (
	"os"
	"regexp"
	"testing"
)

func TestBuildImagesUseTheGoModToolchain(t *testing.T) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^toolchain go(\S+)$`).FindSubmatch(goMod)
	if match == nil {
		t.Fatal("go.mod has no toolchain line")
	}
	version := regexp.QuoteMeta(string(match[1]))
	for file, pattern := range map[string]string{
		"Dockerfile":      `(?m)^FROM golang:` + version + `-alpine AS build$`,
		"cloudbuild.yaml": `(?m)^\s+name: golang:` + version + `$`,
	} {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(pattern).Match(content) {
			t.Errorf("%s must build with golang:%s, the go.mod toolchain", file, match[1])
		}
	}
}
