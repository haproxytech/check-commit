package aspell

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func Test_parseWikiURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "human form with subgroup",
			in:      "https://gitlab.int.haproxy.com/haproxy-controller/gophers/-/wikis/aspell-list-of-words",
			wantURL: "https://gitlab.int.haproxy.com/api/v4/projects/haproxy-controller%2Fgophers/wikis/aspell-list-of-words",
			wantOK:  true,
		},
		{
			name:    "API form passes through",
			in:      "https://gitlab.com/api/v4/projects/grp%2Fproj/wikis/words",
			wantURL: "https://gitlab.com/api/v4/projects/grp%2Fproj/wikis/words",
			wantOK:  true,
		},
		{
			name:   "repo tree URL is not a wiki",
			in:     "https://gitlab.com/grp/proj/-/tree/main/dir",
			wantOK: false,
		},
		{
			name:   "plain API endpoint is not a wiki",
			in:     "https://example.com/api/words",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, ok := parseWikiURL(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && ref.pageURL() != tt.wantURL {
				t.Errorf("pageURL() = %q, want %q", ref.pageURL(), tt.wantURL)
			}
		})
	}
}

func Test_parseWikiContent_render_roundtrip(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantWords []string
		rendered  string // renderWikiContent(wantWords, detected style)
	}{
		{
			name:      "fenced yaml list",
			content:   "```yaml\n- beta\n- alpha\n```",
			wantWords: []string{"beta", "alpha"},
			rendered:  "```yaml\n- beta\n- alpha\n```",
		},
		{
			name:      "plain fence CRLF with dashes",
			content:   "```\r\n- beta\r\n- alpha\r\n```",
			wantWords: []string{"beta", "alpha"},
			rendered:  "```\r\n- beta\r\n- alpha\r\n```",
		},
		{
			name:      "plain newline-separated",
			content:   "beta\nalpha",
			wantWords: []string{"beta", "alpha"},
			rendered:  "beta\nalpha",
		},
		{
			name:      "empty content defaults to plain",
			content:   "",
			wantWords: nil,
			rendered:  "",
		},
		{
			name:      "unclosed fence treated as plain (```yaml line is a word)",
			content:   "```yaml\n- beta\n- alpha",
			wantWords: []string{"```yaml", "beta", "alpha"},
			rendered:  "- ```yaml\n- beta\n- alpha",
		},
		{
			name:      "empty fenced body (closing fence right after opening)",
			content:   "```\n```",
			wantWords: nil,
			rendered:  "```\n```",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words, style := parseWikiContent(tt.content)
			if !slices.Equal(words, tt.wantWords) {
				t.Errorf("words = %q, want %q", words, tt.wantWords)
			}
			if got := renderWikiContent(tt.wantWords, style); got != tt.rendered {
				t.Errorf("render = %q, want %q", got, tt.rendered)
			}
		})
	}
}

func Test_appendToWiki(t *testing.T) {
	tests := []struct {
		name        string
		getStatus   int
		getBody     string
		writeStatus int // response status for write request (default 200)
		words       []string
		wantAdded   []string
		wantSkipped []string
		wantMethod  string // expected write method: PUT or POST
		wantURL     string // expected write request URL path
		wantContent string // content field of the write request
		wantTitle   string // title field in POST payload (empty for PUT)
		wantErr     bool
		wantWritten bool // true if write request should be sent (default true if added)
	}{
		{
			name:        "merge sort and preserve fenced style",
			getStatus:   200,
			getBody:     `{"content": "` + "```" + `\n- zeta\n- alpha\n` + "```" + `"}`,
			writeStatus: 200,
			words:       []string{"Mid", "alpha"},
			wantAdded:   []string{"Mid"},
			wantSkipped: []string{"alpha"},
			wantMethod:  "PUT",
			wantURL:     "/api/v4/projects/grp%2Fproj/wikis/words",
			wantContent: "```\n- alpha\n- Mid\n- zeta\n```",
			wantWritten: true,
		},
		{
			name:        "plain style preserved",
			getStatus:   200,
			getBody:     `{"content": "zeta\nalpha"}`,
			writeStatus: 200,
			words:       []string{"beta"},
			wantAdded:   []string{"beta"},
			wantMethod:  "PUT",
			wantURL:     "/api/v4/projects/grp%2Fproj/wikis/words",
			wantContent: "alpha\nbeta\nzeta",
			wantWritten: true,
		},
		{
			name:        "missing page created via POST",
			getStatus:   404,
			getBody:     `{"message":"404 Wiki Page Not Found"}`,
			writeStatus: 200,
			words:       []string{"beta", "alpha"},
			wantAdded:   []string{"beta", "alpha"},
			wantMethod:  "POST",
			wantURL:     "/api/v4/projects/grp%2Fproj/wikis",
			wantContent: "alpha\nbeta",
			wantTitle:   "words",
			wantWritten: true,
		},
		{
			name:        "GET failure surfaces error",
			getStatus:   500,
			getBody:     `{"message":"boom"}`,
			words:       []string{"beta"},
			wantErr:     true,
			wantWritten: false,
		},
		{
			name:        "write failure surfaces error",
			getStatus:   200,
			getBody:     `{"content": "zeta\nalpha"}`,
			writeStatus: 500,
			words:       []string{"beta"},
			wantErr:     true,
			wantWritten: true,
		},
		{
			name:        "all duplicates: no write request",
			getStatus:   200,
			getBody:     `{"content": "alpha\nBETA"}`,
			words:       []string{"Alpha", "beta"},
			wantAdded:   []string{},
			wantSkipped: []string{"Alpha", "beta"},
			wantWritten: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotContent, gotToken, gotURL, gotTitle string
			var writeRequested bool
			writeStatus := tt.writeStatus
			if writeStatus == 0 {
				writeStatus = 200
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotToken = r.Header.Get("PRIVATE-TOKEN")
				if r.Method == http.MethodGet {
					w.WriteHeader(tt.getStatus)
					_, _ = w.Write([]byte(tt.getBody))
					return
				}
				writeRequested = true
				gotMethod = r.Method
				gotURL = r.URL.EscapedPath()
				var body struct {
					Content string `json:"content"`
					Title   string `json:"title"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				gotContent = body.Content
				gotTitle = body.Title
				w.WriteHeader(writeStatus)
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)

			t.Setenv("TEST_WIKI_TOKEN", "sekrit")
			rf := RemoteFile{PrivateTokenENV: "TEST_WIKI_TOKEN"}
			ref, ok := parseWikiURL(srv.URL + "/grp/proj/-/wikis/words")
			if !ok {
				t.Fatal("parseWikiURL failed")
			}
			added, skipped, err := appendToWiki(rf, ref, tt.words)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("appendToWiki() error = %v", err)
			}
			if !slices.Equal(added, tt.wantAdded) {
				t.Errorf("added = %q, want %q", added, tt.wantAdded)
			}
			if !slices.Equal(skipped, tt.wantSkipped) {
				t.Errorf("skipped = %q, want %q", skipped, tt.wantSkipped)
			}
			if writeRequested != tt.wantWritten {
				t.Errorf("writeRequested = %v, want %v", writeRequested, tt.wantWritten)
			}
			if tt.wantWritten {
				if gotMethod != tt.wantMethod {
					t.Errorf("method = %q, want %q", gotMethod, tt.wantMethod)
				}
				if gotURL != tt.wantURL {
					t.Errorf("URL path = %q, want %q", gotURL, tt.wantURL)
				}
				if gotContent != tt.wantContent {
					t.Errorf("content = %q, want %q", gotContent, tt.wantContent)
				}
				if gotTitle != tt.wantTitle {
					t.Errorf("title = %q, want %q", gotTitle, tt.wantTitle)
				}
			}
			if gotToken != "sekrit" {
				t.Errorf("PRIVATE-TOKEN = %q, want sekrit", gotToken)
			}
		})
	}
}
