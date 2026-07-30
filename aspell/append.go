package aspell

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// Top-level allowed: key, optionally with an inline comment.
	allowedKeyPattern = regexp.MustCompile(`^allowed:\s*(#.*)?$`)
	// Top-level allowed: key with flow-style content, e.g. "allowed: [a, b]".
	allowedFlowKeyPattern = regexp.MustCompile(`^allowed:\s*[^#\s]`)
	// List item line: indent, word, optional inline comment.
	allowedItemPattern = regexp.MustCompile(`^(\s*)-\s+([^#]*?)\s*(#.*)?$`)
)

// normalizeWord lowercases and unquotes for case-insensitive comparison.
func normalizeWord(w string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(w), `'"`))
}

// itemWord extracts the word token from an item line for sorting.
func itemWord(line string) string {
	m := allowedItemPattern.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	return strings.TrimSpace(m[2])
}

func isItemLine(line string) bool {
	m := allowedItemPattern.FindStringSubmatch(line)
	return m != nil && strings.TrimSpace(m[2]) != ""
}

func isCommentOrBlank(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// findAllowedBlock returns the [start,end) line range of allowed: items.
// end only advances past comments/blanks when another item follows.
func findAllowedBlock(lines []string) (start, end int) {
	keyIdx := -1
	for i, line := range lines {
		if allowedKeyPattern.MatchString(line) {
			keyIdx = i
			break
		}
	}
	if keyIdx == -1 {
		return -1, -1
	}
	start = keyIdx + 1
	end = start
	for j := start; j < len(lines); j++ {
		switch {
		case isItemLine(lines[j]):
			end = j + 1
		case isCommentOrBlank(lines[j]):
			continue
		default:
			return start, end
		}
	}
	return start, end
}

func sortItems(items []string) {
	slices.SortStableFunc(items, func(a, b string) int {
		return strings.Compare(normalizeWord(itemWord(a)), normalizeWord(itemWord(b)))
	})
}

// appendLocal merges words into the allowed: block, sorted, comments kept.
func appendLocal(filename string, data []byte, words []string) (added, skipped []string, err error) {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	if slices.ContainsFunc(lines, allowedFlowKeyPattern.MatchString) {
		return nil, nil, errors.New("flow-style allowed list not supported; convert to block style")
	}
	start, end := findAllowedBlock(lines)

	existing := map[string]bool{}
	var items, comments []string
	indent := "  "
	if start >= 0 {
		for _, line := range lines[start:end] {
			if isItemLine(line) {
				items = append(items, line)
				existing[normalizeWord(itemWord(line))] = true
				indent = allowedItemPattern.FindStringSubmatch(line)[1]
			} else {
				comments = append(comments, line)
			}
		}
	}

	for _, w := range words {
		if existing[normalizeWord(w)] {
			skipped = append(skipped, w)
			continue
		}
		existing[normalizeWord(w)] = true
		items = append(items, indent+"- "+w)
		added = append(added, w)
	}

	if len(added) == 0 {
		return added, skipped, nil // nothing to write
	}
	sortItems(items)

	var out []string
	if start >= 0 {
		out = append(out, lines[:start]...)
		out = append(out, comments...)
		out = append(out, items...)
		out = append(out, lines[end:]...)
	} else {
		out = append(out, lines...)
		out = append(out, "allowed:")
		out = append(out, items...)
	}
	content := strings.Join(out, "\n") + "\n"
	return added, skipped, os.WriteFile(filename, []byte(content), 0o644)
}

// Append adds words to the allowed list: local .aspell.yml, or a GitLab
// wiki page when remote_file points at one. Other remotes are refused to
// keep a single source of truth.
func Append(filename string, words []string) (added, skipped []string, err error) {
	cleaned := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			return nil, nil, errors.New("empty word argument")
		}
		// Reject characters that would corrupt the YAML list item on write.
		if strings.ContainsAny(w, " \t\n\r:#'\"") || strings.HasPrefix(w, "-") {
			return nil, nil, fmt.Errorf("invalid word %q", w)
		}
		cleaned = append(cleaned, w)
	}

	data, readErr := os.ReadFile(filename)
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, nil, readErr
	}

	var cfg Aspell
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, err
	}

	remoteURL := cfg.RemoteFile.URL
	if cfg.RemoteFile.URLEnv != "" {
		remoteURL = os.Getenv(cfg.RemoteFile.URLEnv)
		if remoteURL == "" {
			// Env var configured but unset/empty: never fall back to a local
			// edit, or the local file forks from the single source of truth.
			return nil, nil, fmt.Errorf(
				"remote_file url_env %s is set but the variable is empty; refusing to append locally",
				cfg.RemoteFile.URLEnv)
		}
	}
	if remoteURL != "" {
		if ref, ok := parseWikiURL(remoteURL); ok {
			return appendToWiki(cfg.RemoteFile, ref, cleaned)
		}
		return nil, nil, fmt.Errorf(
			"allowed words are managed remotely; add them there instead: %s", remoteURL)
	}
	d := cfg.Dictionaries
	if len(d.GitHub) > 0 || len(d.GitLab) > 0 || len(d.URLs) > 0 {
		var sources []string
		for _, gh := range d.GitHub {
			sources = append(sources, gh.URL)
		}
		for _, gl := range d.GitLab {
			sources = append(sources, gl.URL)
		}
		sources = append(sources, d.URLs...)
		return nil, nil, fmt.Errorf(
			"allowed words are managed by remote dictionaries; add them there instead: %s",
			strings.Join(sources, ", "))
	}
	return appendLocal(filename, data, cleaned)
}
