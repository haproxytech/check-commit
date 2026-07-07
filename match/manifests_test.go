package match

import (
	"slices"
	"testing"
)

func TestGetGoModWords(t *testing.T) {
	content := `module github.com/haproxytech/check-commit/v5

go 1.24

require (
	github.com/fatih/camelcase v1.0.0
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require golang.org/x/oauth2 v0.21.0
`
	words := GetGoModWords(content)
	for _, want := range []string{"github", "haproxytech", "fatih", "camelcase", "gopkg", "yaml", "golang", "oauth2"} {
		if !slices.Contains(words, want) {
			t.Errorf("GetGoModWords() missing %q, got %v", want, words)
		}
	}
	if slices.Contains(words, "v") {
		t.Errorf("GetGoModWords() must not contain single-letter tokens, got %v", words)
	}
}

func TestGetPackageJSONWords(t *testing.T) {
	content := `{
  "name": "@haproxytech/kubernetes-ingress-ui",
  "dependencies": {
    "vue-router": "^4.0.0",
    "pinia": "^2.1.0"
  },
  "devDependencies": {
    "eslint": "^9.0.0"
  }
}`
	words := GetPackageJSONWords(content)
	for _, want := range []string{"haproxytech", "kubernetes", "ingress", "ui", "vue", "router", "pinia", "eslint"} {
		if !slices.Contains(words, want) {
			t.Errorf("GetPackageJSONWords() missing %q, got %v", want, words)
		}
	}
	if got := GetPackageJSONWords("{not json"); got != nil {
		t.Errorf("GetPackageJSONWords() on malformed input = %v, want nil", got)
	}
}

func TestGetRequirementsWords(t *testing.T) {
	content := `# comment
requests>=2.31
pydantic[email]==2.7.0
-r other-requirements.txt

flask
`
	words := GetRequirementsWords(content)
	for _, want := range []string{"requests", "pydantic", "flask"} {
		if !slices.Contains(words, want) {
			t.Errorf("GetRequirementsWords() missing %q, got %v", want, words)
		}
	}
	if slices.Contains(words, "email") {
		t.Errorf("GetRequirementsWords() must not include extras, got %v", words)
	}
}

func TestGetCargoTomlWords(t *testing.T) {
	content := `[package]
name = "my-app"

[dependencies]
serde = { version = "1", features = ["derive"] }
tokio = "1.38"

[dev-dependencies]
criterion = "0.5"

[profile.release]
lto = true
`
	words := GetCargoTomlWords(content)
	for _, want := range []string{"serde", "tokio", "criterion"} {
		if !slices.Contains(words, want) {
			t.Errorf("GetCargoTomlWords() missing %q, got %v", want, words)
		}
	}
	if slices.Contains(words, "lto") {
		t.Errorf("GetCargoTomlWords() must only read dependency sections, got %v", words)
	}
}

func TestGetPathWords(t *testing.T) {
	paths := []string{
		"aspell/fetch_dictionaries.go",
		"docs/config-template.md",
		".github/workflows/ci.yml",
	}
	words := GetPathWords(paths)
	for _, want := range []string{"aspell", "fetch", "dictionaries", "docs", "config", "template", "github", "workflows", "ci", "yml"} {
		if !slices.Contains(words, want) {
			t.Errorf("GetPathWords() missing %q, got %v", want, words)
		}
	}
	if slices.Contains(words, "go") {
		t.Errorf("GetPathWords() must filter common keywords, got %v", words)
	}
}
