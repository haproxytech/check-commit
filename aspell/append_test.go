package aspell

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".aspell.yml")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func Test_appendLocal(t *testing.T) {
	tests := []struct {
		name        string
		file        string // "" = missing file
		words       []string
		wantAdded   []string
		wantSkipped []string
		wantFile    string
	}{
		{
			name:      "missing file created",
			file:      "",
			words:     []string{"zeta", "Alpha"},
			wantAdded: []string{"zeta", "Alpha"},
			wantFile:  "allowed:\n  - Alpha\n  - zeta\n",
		},
		{
			name:      "existing unsorted list gets fully sorted",
			file:      "mode: all\nallowed:\n  - zeta\n  - alpha\n",
			words:     []string{"mid"},
			wantAdded: []string{"mid"},
			wantFile:  "mode: all\nallowed:\n  - alpha\n  - mid\n  - zeta\n",
		},
		{
			name:        "duplicate is skipped case-insensitively",
			file:        "allowed:\n  - Alpha\n",
			words:       []string{"alpha", "beta"},
			wantAdded:   []string{"beta"},
			wantSkipped: []string{"alpha"},
			wantFile:    "allowed:\n  - Alpha\n  - beta\n",
		},
		{
			name:      "file without allowed key gets block appended",
			file:      "mode: all\nmin_length: 3\n",
			words:     []string{"word"},
			wantAdded: []string{"word"},
			wantFile:  "mode: all\nmin_length: 3\nallowed:\n  - word\n",
		},
		{
			name:      "init template: commented allowed not matched, block appended",
			file:      "## Extra words to accept\n# allowed:\n#   - example\n",
			words:     []string{"word"},
			wantAdded: []string{"word"},
			wantFile:  "## Extra words to accept\n# allowed:\n#   - example\nallowed:\n  - word\n",
		},
		{
			name:      "inline comment travels with its word",
			file:      "allowed:\n  - zeta # ours\n  - alpha\n",
			words:     []string{"beta"},
			wantAdded: []string{"beta"},
			wantFile:  "allowed:\n  - alpha\n  - beta\n  - zeta # ours\n",
		},
		{
			name:      "standalone comment stays above sorted items",
			file:      "allowed:\n  - zeta\n  # legacy words\n  - alpha\n",
			words:     []string{"beta"},
			wantAdded: []string{"beta"},
			wantFile:  "allowed:\n  # legacy words\n  - alpha\n  - beta\n  - zeta\n",
		},
		{
			name:      "following section untouched",
			file:      "allowed:\n  - zeta\n\n## next section\nignore_files:\n  - '*test.go'\n",
			words:     []string{"alpha"},
			wantAdded: []string{"alpha"},
			wantFile:  "allowed:\n  - alpha\n  - zeta\n\n## next section\nignore_files:\n  - '*test.go'\n",
		},
		{
			name:        "all duplicates: file untouched, no error",
			file:        "allowed:\n  - zeta\n  - alpha\n",
			words:       []string{"ZETA"},
			wantSkipped: []string{"ZETA"},
			wantFile:    "allowed:\n  - zeta\n  - alpha\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.file)
			var data []byte
			if tt.file != "" {
				data = []byte(tt.file)
			}
			added, skipped, err := appendLocal(path, data, tt.words)
			if err != nil {
				t.Fatalf("appendLocal() error = %v", err)
			}
			if !slices.Equal(added, tt.wantAdded) {
				t.Errorf("added = %q, want %q", added, tt.wantAdded)
			}
			if !slices.Equal(skipped, tt.wantSkipped) {
				t.Errorf("skipped = %q, want %q", skipped, tt.wantSkipped)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantFile {
				t.Errorf("file:\n%s\nwant:\n%s", got, tt.wantFile)
			}
		})
	}
}

func Test_Append_routing(t *testing.T) {
	t.Run("non-wiki remote_file refuses with URL", func(t *testing.T) {
		path := writeTemp(t, "remote_file:\n  url: https://example.com/api/words\n  allowed_items_key: words\n")
		_, _, err := Append(path, []string{"word"})
		if err == nil || !strings.Contains(err.Error(), "https://example.com/api/words") {
			t.Fatalf("want refusal containing URL, got %v", err)
		}
	})
	t.Run("url_env resolved before routing", func(t *testing.T) {
		t.Setenv("TEST_WORDS_URL", "https://example.com/api/words")
		path := writeTemp(t, "remote_file:\n  url_env: TEST_WORDS_URL\n")
		_, _, err := Append(path, []string{"word"})
		if err == nil || !strings.Contains(err.Error(), "https://example.com/api/words") {
			t.Fatalf("want refusal containing resolved URL, got %v", err)
		}
	})
	t.Run("dictionaries-only refuses", func(t *testing.T) {
		path := writeTemp(t, "dictionaries:\n  urls:\n    - https://example.com/words.txt\n")
		_, _, err := Append(path, []string{"word"})
		if err == nil || !strings.Contains(err.Error(), "https://example.com/words.txt") {
			t.Fatalf("want refusal containing dictionary URL, got %v", err)
		}
	})
	t.Run("empty word rejected", func(t *testing.T) {
		path := writeTemp(t, "")
		if _, _, err := Append(path, []string{"  "}); err == nil {
			t.Fatal("want error for empty word")
		}
	})
	t.Run("invalid words rejected", func(t *testing.T) {
		const original = "allowed:\n  - alpha\n"
		path := writeTemp(t, original)
		for _, w := range []string{"foo: bar", "a\nb", "#lead", "-dash"} {
			_, _, err := Append(path, []string{w})
			if err == nil || !strings.Contains(err.Error(), "invalid word") {
				t.Errorf("word %q: want invalid word error, got %v", w, err)
			}
		}
		got, _ := os.ReadFile(path)
		if string(got) != original {
			t.Fatal("file must stay untouched when words are invalid")
		}
	})
	t.Run("flow-style allowed list rejected", func(t *testing.T) {
		const original = "mode: all\nallowed: [foo, bar]\n"
		path := writeTemp(t, original)
		_, _, err := Append(path, []string{"baz"})
		if err == nil || !strings.Contains(err.Error(), "flow-style") {
			t.Fatalf("want flow-style error, got %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != original {
			t.Fatal("file must stay untouched on flow-style allowed list")
		}
	})
	t.Run("url_env set but empty refuses local fallback", func(t *testing.T) {
		const original = "remote_file:\n  url_env: TEST_MISSING_WORDS_URL\n"
		path := writeTemp(t, original)
		_, _, err := Append(path, []string{"word"})
		if err == nil || !strings.Contains(err.Error(), "TEST_MISSING_WORDS_URL") {
			t.Fatalf("want error naming env var, got %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != original {
			t.Fatal("file must stay untouched")
		}
	})
	t.Run("no remote goes local", func(t *testing.T) {
		path := writeTemp(t, "allowed:\n  - zeta\n")
		added, _, err := Append(path, []string{"alpha"})
		if err != nil || !slices.Equal(added, []string{"alpha"}) {
			t.Fatalf("added = %q, err = %v", added, err)
		}
	})
	t.Run("wiki remote updates page not file", func(t *testing.T) {
		var wrote bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				wrote = true
			}
			_, _ = w.Write([]byte(`{"content": "zeta"}`))
		}))
		t.Cleanup(srv.Close)
		cfg := "remote_file:\n  url: " + srv.URL + "/grp/proj/-/wikis/words\n"
		path := writeTemp(t, cfg)
		added, _, err := Append(path, []string{"alpha"})
		if err != nil || !slices.Equal(added, []string{"alpha"}) {
			t.Fatalf("added = %q, err = %v", added, err)
		}
		if !wrote {
			t.Fatal("expected PUT to wiki")
		}
		got, _ := os.ReadFile(path)
		if string(got) != cfg {
			t.Fatal(".aspell.yml must stay untouched on wiki path")
		}
	})
}

func Test_normalizeWord(t *testing.T) {
	for in, want := range map[string]string{
		"Alpha": "alpha", "'mri'": "mri", `"rws"`: "rws",
	} {
		if got := normalizeWord(in); got != want {
			t.Errorf("normalizeWord(%q) = %q, want %q", in, got, want)
		}
	}
}
