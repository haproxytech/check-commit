package aspell

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/haproxytech/check-commit/v5/match"
)

// collectManifestWords reads known dependency manifests from dir and
// extracts allowed words from package/module names. Missing files are
// skipped; a word-source problem never fails the run.
func collectManifestWords(dir string) []string {
	manifests := []struct {
		name    string
		extract func(string) []string
	}{
		{"go.mod", match.GetGoModWords},
		{"package.json", match.GetPackageJSONWords},
		{"requirements.txt", match.GetRequirementsWords},
		{"Cargo.toml", match.GetCargoTomlWords},
	}
	var words []string
	for _, m := range manifests {
		data, err := os.ReadFile(filepath.Join(dir, m.name))
		if err != nil {
			continue
		}
		extracted := m.extract(string(data))
		words = append(words, extracted...)
		slog.Info("collected words from manifest", "count", len(extracted), "file", m.name)
	}
	return words
}

// collectPathWords walks the repository and extracts words from path
// segments only; file contents are not read. Skip rules match
// identifierScopeAll.
func collectPathWords(root string) []string {
	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	words := match.GetPathWords(paths)
	slog.Info("collected words from repository paths", "count", len(words), "paths", len(paths))
	return words
}
