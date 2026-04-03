package aspell

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DictionaryConfig holds configuration for fetching dictionaries from remote sources.
type DictionaryConfig struct {
	// GitHub repo directories to fetch all dictionary files from.
	// Supports URLs like https://github.com/owner/repo/tree/branch/path/to/dir
	GitHub []GitHubDictionary `yaml:"github"`
	// GitLab repo directories to fetch all dictionary files from.
	// Supports URLs like https://gitlab.com/group/project/-/tree/branch/path/to/dir
	GitLab []GitLabDictionary `yaml:"gitlab"`
	// Direct URLs to individual dictionary files (.txt or .rws).
	URLs []string `yaml:"urls"`
}

// GitHubDictionary configures fetching dictionaries from a GitHub repository directory.
type GitHubDictionary struct {
	// URL to a GitHub directory, e.g. https://github.com/haproxytech/check-commit/tree/main/aspell/dictionaries
	URL string `yaml:"url"`
	// Optional environment variable name containing a GitHub token for private repos.
	TokenEnv string `yaml:"token_env"`
}

// GitLabDictionary configures fetching dictionaries from a GitLab repository directory.
type GitLabDictionary struct {
	// URL to a GitLab directory, e.g. https://gitlab.com/group/project/-/tree/main/path/to/dir
	// Also supports self-hosted instances like https://gitlab.example.com/group/project/-/tree/main/path
	URL string `yaml:"url"`
	// Optional environment variable name containing a GitLab private token.
	TokenEnv string `yaml:"token_env"`
}

// githubTreePattern matches URLs like https://github.com/{owner}/{repo}/tree/{ref}/{path}
var githubTreePattern = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/tree/([^/]+)/(.+)$`)

// githubRawPattern matches raw.githubusercontent.com URLs.
var githubRawPattern = regexp.MustCompile(`^https://raw\.githubusercontent\.com/`)

// gitlabTreePattern matches URLs like https://{host}/{project-path}/-/tree/{ref}/{path}
// The project path can contain multiple segments (groups/subgroups).
var gitlabTreePattern = regexp.MustCompile(`^(https://[^/]+)/(.+?)/-/tree/([^/]+)/(.+)$`)

// isDictionaryFile returns true if the filename has a supported dictionary extension.
func isDictionaryFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".rws")
}

type fetchedDictionaries struct {
	words    []string // words from .txt files
	rwsFiles []string // local paths to downloaded .rws files
}

// fetchDictionaries fetches dictionaries from all configured sources.
func fetchDictionaries(cfg DictionaryConfig) (fetchedDictionaries, error) {
	var result fetchedDictionaries

	for _, gh := range cfg.GitHub {
		fetched, err := fetchGitHubDirectory(gh)
		if err != nil {
			return result, fmt.Errorf("fetching github dictionary %q: %w", gh.URL, err)
		}
		result.words = append(result.words, fetched.words...)
		result.rwsFiles = append(result.rwsFiles, fetched.rwsFiles...)
	}

	for _, gl := range cfg.GitLab {
		fetched, err := fetchGitLabDirectory(gl)
		if err != nil {
			return result, fmt.Errorf("fetching gitlab dictionary %q: %w", gl.URL, err)
		}
		result.words = append(result.words, fetched.words...)
		result.rwsFiles = append(result.rwsFiles, fetched.rwsFiles...)
	}

	for _, rawURL := range cfg.URLs {
		fetched, err := fetchDictionaryURL(rawURL)
		if err != nil {
			return result, fmt.Errorf("fetching dictionary URL %q: %w", rawURL, err)
		}
		result.words = append(result.words, fetched.words...)
		result.rwsFiles = append(result.rwsFiles, fetched.rwsFiles...)
	}

	return result, nil
}

// fetchGitHubDirectory fetches all dictionary files from a GitHub repo directory.
func fetchGitHubDirectory(gh GitHubDictionary) (fetchedDictionaries, error) {
	var result fetchedDictionaries

	m := githubTreePattern.FindStringSubmatch(gh.URL)
	if m == nil {
		return result, fmt.Errorf("invalid GitHub directory URL %q, expected https://github.com/{owner}/{repo}/tree/{ref}/{path}", gh.URL)
	}
	owner, repo, ref, path := m[1], m[2], m[3], m[4]

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, ref)
	log.Printf("aspell dictionaries: listing GitHub directory: %s", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if gh.TokenEnv != "" {
		token := os.Getenv(gh.TokenEnv)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			log.Printf("aspell dictionaries: warning: token env %s is empty", gh.TokenEnv)
		}
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("GitHub API returned %s for %s", resp.Status, apiURL)
	}

	var entries []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return result, fmt.Errorf("decoding GitHub API response: %w", err)
	}

	for _, entry := range entries {
		if !isDictionaryFile(entry.Name) {
			continue
		}
		if entry.DownloadURL == "" {
			continue
		}
		log.Printf("aspell dictionaries: fetching %s", entry.DownloadURL)
		fetched, err := fetchSingleFile(entry.DownloadURL, entry.Name, gh.TokenEnv)
		if err != nil {
			return result, fmt.Errorf("fetching %s: %w", entry.Name, err)
		}
		result.words = append(result.words, fetched.words...)
		result.rwsFiles = append(result.rwsFiles, fetched.rwsFiles...)
	}

	log.Printf("aspell dictionaries: loaded %d words and %d .rws files from %s", len(result.words), len(result.rwsFiles), gh.URL)
	return result, nil
}

// fetchGitLabDirectory fetches all dictionary files from a GitLab repo directory.
// It uses the GitLab Repository Tree API to list files, then fetches each one
// via the Repository Files raw endpoint.
func fetchGitLabDirectory(gl GitLabDictionary) (fetchedDictionaries, error) {
	var result fetchedDictionaries

	m := gitlabTreePattern.FindStringSubmatch(gl.URL)
	if m == nil {
		return result, fmt.Errorf("invalid GitLab directory URL %q, expected https://{host}/{project}/-/tree/{ref}/{path}", gl.URL)
	}
	baseURL, project, ref, path := m[1], m[2], m[3], m[4]

	encodedProject := strings.ReplaceAll(project, "/", "%2F")
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/tree?path=%s&ref=%s&per_page=100",
		baseURL, encodedProject, path, ref)
	log.Printf("aspell dictionaries: listing GitLab directory: %s", apiURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return result, err
	}
	setGitLabToken(req, gl.TokenEnv)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("GitLab API returned %s for %s", resp.Status, apiURL)
	}

	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"` // "blob" for files
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return result, fmt.Errorf("decoding GitLab API response: %w", err)
	}

	for _, entry := range entries {
		if entry.Type != "blob" || !isDictionaryFile(entry.Name) {
			continue
		}
		encodedPath := strings.ReplaceAll(entry.Path, "/", "%2F")
		rawURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s/raw?ref=%s",
			baseURL, encodedProject, encodedPath, ref)
		log.Printf("aspell dictionaries: fetching %s", rawURL)
		fetched, err := fetchSingleFile(rawURL, entry.Name, gl.TokenEnv)
		if err != nil {
			return result, fmt.Errorf("fetching %s: %w", entry.Name, err)
		}
		result.words = append(result.words, fetched.words...)
		result.rwsFiles = append(result.rwsFiles, fetched.rwsFiles...)
	}

	log.Printf("aspell dictionaries: loaded %d words and %d .rws files from %s", len(result.words), len(result.rwsFiles), gl.URL)
	return result, nil
}

func setGitLabToken(req *http.Request, tokenEnv string) {
	if tokenEnv == "" {
		return
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		log.Printf("aspell dictionaries: warning: token env %s is empty", tokenEnv)
		return
	}
	req.Header.Set("PRIVATE-TOKEN", token)
}

// fetchDictionaryURL fetches a single dictionary file from a URL.
func fetchDictionaryURL(rawURL string) (fetchedDictionaries, error) {
	name := filepath.Base(rawURL)
	log.Printf("aspell dictionaries: fetching %s", rawURL)
	return fetchSingleFile(rawURL, name, "")
}

// fetchSingleFile downloads a file and processes it based on extension.
func fetchSingleFile(url, name, tokenEnv string) (fetchedDictionaries, error) {
	var result fetchedDictionaries

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return result, err
	}
	if tokenEnv != "" {
		token := os.Getenv(tokenEnv)
		if token != "" {
			if githubRawPattern.MatchString(url) {
				req.Header.Set("Authorization", "Bearer "+token)
			} else if strings.Contains(url, "/api/v4/") {
				req.Header.Set("PRIVATE-TOKEN", token)
			}
		}
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("HTTP %s fetching %s", resp.Status, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, err
	}

	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".rws"):
		// Save .rws to temp file for aspell --extra-dicts
		tmpFile, err := os.CreateTemp("", "aspell-dict-*.rws")
		if err != nil {
			return result, fmt.Errorf("creating temp file for %s: %w", name, err)
		}
		if _, err := tmpFile.Write(data); err != nil {
			tmpFile.Close()
			return result, fmt.Errorf("writing temp file for %s: %w", name, err)
		}
		tmpFile.Close()
		result.rwsFiles = append(result.rwsFiles, tmpFile.Name())
		log.Printf("aspell dictionaries: saved .rws dictionary %s to %s", name, tmpFile.Name())
	case strings.HasSuffix(lower, ".txt"):
		words := parseTxtDictionary(string(data))
		result.words = append(result.words, words...)
		log.Printf("aspell dictionaries: loaded %d words from %s", len(words), name)
	default:
		// Treat unknown extensions as plain text word lists
		words := parseTxtDictionary(string(data))
		result.words = append(result.words, words...)
		log.Printf("aspell dictionaries: loaded %d words from %s (treated as text)", len(words), name)
	}

	return result, nil
}

// parseTxtDictionary parses a text dictionary file (one word per line, # comments).
func parseTxtDictionary(content string) []string {
	var words []string
	for line := range strings.SplitSeq(content, "\n") {
		word := strings.TrimSpace(line)
		if word == "" || strings.HasPrefix(word, "#") {
			continue
		}
		words = append(words, strings.ToLower(word))
	}
	return words
}
