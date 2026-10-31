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

func TestBuildConfigsPassEverySettingTheDeployScriptRequires(t *testing.T) {
	script, err := os.ReadFile("scripts/deploy-cloud-run.sh")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)request\)\s+for name in \\\n(.*?); do\s+require_value`).FindSubmatch(script)
	if block == nil {
		t.Fatal("deploy-cloud-run.sh has no request-mode require_value list")
	}
	required := regexp.MustCompile(`[A-Z][A-Z0-9_]+`).FindAll(block[1], -1)
	if len(required) == 0 {
		t.Fatal("request-mode require_value list is empty")
	}
	for _, file := range []string{"cloudbuild.yaml", "cloudbuild.deploy.yaml"} {
		config, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range required {
			if !regexp.MustCompile(`(?m)^\s+- ` + string(name) + `=\S`).Match(config) {
				t.Errorf("%s does not pass %s, which deploy-cloud-run.sh requires", file, name)
			}
		}
	}
}
