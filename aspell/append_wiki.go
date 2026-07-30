package aspell

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
)

// wikiRef locates a GitLab wiki page via the API.
type wikiRef struct {
	apiBase     string // https://host/api/v4/projects/{proj}/wikis
	slug        string // human-readable page title
	slugEscaped string
}

func (r wikiRef) pageURL() string {
	return r.apiBase + "/" + r.slugEscaped
}

var (
	wikiHumanPattern = regexp.MustCompile(`^(https?://[^/]+)/(.+?)/-/wikis/([^?#]+)$`)
	wikiAPIPattern   = regexp.MustCompile(`^(https?://[^/]+)/api/v4/projects/([^/]+)/wikis/([^?#]+)$`)
)

// parseWikiURL accepts human (/-/wikis/) and API wiki URLs.
func parseWikiURL(raw string) (wikiRef, bool) {
	if m := wikiAPIPattern.FindStringSubmatch(raw); m != nil {
		slug, err := url.PathUnescape(m[3])
		if err != nil {
			slug = m[3]
		}
		return wikiRef{
			apiBase:     fmt.Sprintf("%s/api/v4/projects/%s/wikis", m[1], m[2]),
			slug:        slug,
			slugEscaped: m[3],
		}, true
	}
	if m := wikiHumanPattern.FindStringSubmatch(raw); m != nil {
		return wikiRef{
			apiBase:     fmt.Sprintf("%s/api/v4/projects/%s/wikis", m[1], url.PathEscape(m[2])),
			slug:        m[3],
			slugEscaped: url.PathEscape(m[3]),
		}, true
	}
	return wikiRef{}, false
}

// wikiStyle records page formatting so writes preserve it.
type wikiStyle struct {
	fence  string // opening fence line; "" = no fence
	dashed bool
	crlf   bool
}

// parseWikiContent extracts words and detects the page's list style.
func parseWikiContent(raw string) ([]string, wikiStyle) {
	style := wikiStyle{crlf: strings.Contains(raw, "\r\n")}
	content := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if strings.HasPrefix(content, "```") {
		fence, _, _ := strings.Cut(content, "\n")
		stripped := stripCodeFence(content)
		if stripped != content {
			// Fence was closed; use stripped content and record fence style.
			style.fence = fence
			content = stripped
		}
		// Unclosed fence: treat as plain text, fence line becomes a word.
	}
	var words []string
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "- "); ok {
			style.dashed = true
			line = strings.TrimSpace(rest)
		}
		words = append(words, line)
	}
	return words, style
}

// renderWikiContent writes words back in the detected style.
func renderWikiContent(words []string, style wikiStyle) string {
	var b strings.Builder
	nl := "\n"
	if style.crlf {
		nl = "\r\n"
	}
	if style.fence != "" {
		b.WriteString(style.fence)
		b.WriteString(nl)
	}
	for _, w := range words {
		if style.dashed {
			b.WriteString("- ")
		}
		b.WriteString(w)
		b.WriteString(nl)
	}
	if style.fence != "" {
		b.WriteString("```")
	}
	return strings.TrimSuffix(b.String(), nl)
}

// setWikiAuth mirrors fetchRemoteFile's auth handling, with glab fallback.
func setWikiAuth(req *http.Request, rf RemoteFile) {
	if rf.HeaderFromENV != "" {
		req.Header.Set(rf.HeaderFromENV, os.Getenv(rf.HeaderFromENV))
	}
	if token := gitlabToken(rf.PrivateTokenENV, req.URL.Hostname()); token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
}

// appendToWiki merges words into the wiki page, sorted, style preserved.
func appendToWiki(rf RemoteFile, ref wikiRef, words []string) (added, skipped []string, err error) {
	req, err := http.NewRequest(http.MethodGet, ref.pageURL(), nil)
	if err != nil {
		return nil, nil, err
	}
	setWikiAuth(req, rf)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var current []string
	style := wikiStyle{}
	create := false
	switch resp.StatusCode {
	case http.StatusOK:
		key := rf.AllowedItemsKey
		if key == "" {
			key = "content"
		}
		var data map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, nil, fmt.Errorf("wiki page: failed to decode JSON: %w", err)
		}
		content, ok := data[key].(string)
		if !ok {
			return nil, nil, fmt.Errorf("wiki page: key %q not found or not a string", key)
		}
		current, style = parseWikiContent(content)
	case http.StatusNotFound:
		create = true // new page: plain newline-separated
	default:
		return nil, nil, fmt.Errorf("wiki page: GET returned %s", resp.Status)
	}

	existing := map[string]bool{}
	for _, w := range current {
		existing[normalizeWord(w)] = true
	}
	merged := append([]string{}, current...)
	for _, w := range words {
		if existing[normalizeWord(w)] {
			skipped = append(skipped, w)
			continue
		}
		existing[normalizeWord(w)] = true
		merged = append(merged, w)
		added = append(added, w)
	}
	if len(added) == 0 {
		return added, skipped, nil
	}
	slices.SortStableFunc(merged, func(a, b string) int {
		return strings.Compare(normalizeWord(a), normalizeWord(b))
	})

	payload := map[string]string{
		"content": renderWikiContent(merged, style),
		"format":  "markdown",
	}
	method, writeURL := http.MethodPut, ref.pageURL()
	if create {
		method, writeURL = http.MethodPost, ref.apiBase
		payload["title"] = ref.slug
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	wreq, err := http.NewRequest(method, writeURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	wreq.Header.Set("Content-Type", "application/json")
	setWikiAuth(wreq, rf)
	wresp, err := (&http.Client{}).Do(wreq)
	if err != nil {
		return nil, nil, err
	}
	defer wresp.Body.Close()
	if wresp.StatusCode < 200 || wresp.StatusCode > 299 {
		return nil, nil, fmt.Errorf("wiki page: %s returned %s", method, wresp.Status)
	}
	return added, skipped, nil
}
