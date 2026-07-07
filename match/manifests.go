package match

import (
	"encoding/json"
	"regexp"
	"strings"
)

// addSeparatedName splits a package/module name on path-style separators
// and feeds each segment through addIdent.
func addSeparatedName(seen map[string]struct{}, name string) {
	for _, seg := range strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == '.' || r == '@'
	}) {
		addIdent(seen, seg)
	}
}

func setToSlice(seen map[string]struct{}) []string {
	result := make([]string, 0, len(seen))
	for word := range seen {
		result = append(result, word)
	}
	return result
}

// GetGoModWords extracts words from module paths in go.mod content:
// the module line and all require directives (block and single-line).
func GetGoModWords(content string) []string {
	seen := map[string]struct{}{}
	inRequire := false
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "module "):
			addSeparatedName(seen, strings.TrimSpace(strings.TrimPrefix(line, "module ")))
		case strings.HasPrefix(line, "require ("):
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire, strings.HasPrefix(line, "require "):
			fields := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(fields) > 0 && !strings.HasPrefix(fields[0], "//") {
				addSeparatedName(seen, fields[0])
			}
		}
	}
	return setToSlice(seen)
}

// GetPackageJSONWords extracts words from the package name and the keys of
// dependencies and devDependencies. Returns nil on malformed JSON.
func GetPackageJSONWords(content string) []string {
	var pkg struct {
		Name            string            `json:"name"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	addSeparatedName(seen, pkg.Name)
	for name := range pkg.Dependencies {
		addSeparatedName(seen, name)
	}
	for name := range pkg.DevDependencies {
		addSeparatedName(seen, name)
	}
	return setToSlice(seen)
}

// requirementNameRe matches the package name at the start of a requirements
// line, stopping before version specifiers and extras.
var requirementNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+`)

// GetRequirementsWords extracts package names from requirements.txt content,
// skipping comments and pip option lines (-r, -e, --hash, ...).
func GetRequirementsWords(content string) []string {
	seen := map[string]struct{}{}
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if name := requirementNameRe.FindString(line); name != "" {
			addSeparatedName(seen, name)
		}
	}
	return setToSlice(seen)
}

// GetCargoTomlWords extracts crate names from dependency sections of
// Cargo.toml content ([dependencies], [dev-dependencies], and nested
// variants like [workspace.dependencies]).
func GetCargoTomlWords(content string) []string {
	seen := map[string]struct{}{}
	inDeps := false
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section := strings.Trim(line, "[]")
			inDeps = section == "dependencies" || section == "dev-dependencies" ||
				strings.HasSuffix(section, ".dependencies") || strings.HasSuffix(section, ".dev-dependencies")
			continue
		}
		if !inDeps || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		addSeparatedName(seen, strings.Trim(strings.TrimSpace(name), `"`))
	}
	return setToSlice(seen)
}

// GetPathWords extracts words from repository path segments: directories,
// file base names, and extensions.
func GetPathWords(paths []string) []string {
	seen := map[string]struct{}{}
	for _, p := range paths {
		for _, seg := range strings.FieldsFunc(p, func(r rune) bool {
			return r == '/' || r == '\\' || r == '.'
		}) {
			addIdent(seen, seg)
		}
	}
	return setToSlice(seen)
}
