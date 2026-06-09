package aspell

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/haproxytech/check-commit/v5/junit"
	"github.com/haproxytech/check-commit/v5/match"

	"github.com/fatih/camelcase"
)

// Commit carries the data needed to spell-check a single commit and to
// report which commit an error came from.
type Commit struct {
	Hash    string
	Subject string
	Message string
}

type RemoteFile struct {
	URL             string `yaml:"url"`
	URLEnv          string `yaml:"url_env"`
	HeaderFromENV   string `yaml:"header_from_env"`
	PrivateTokenENV string `yaml:"private_token_env"`
	AllowedItemsKey string `yaml:"allowed_items_key"`
}

type Aspell struct {
	RemoteFile          RemoteFile       `yaml:"remote_file"`
	Mode                mode             `yaml:"mode"`
	HelpText            string           `yaml:"-"`
	IdentifierScope     identifierScope  `yaml:"identifier_scope"`
	Dictionaries        DictionaryConfig `yaml:"dictionaries"`
	IgnoreFiles         []string         `yaml:"ignore_files"`
	AllowedWords        []string         `yaml:"allowed"`
	ExtraDicts          []string         `yaml:"-"` // paths to downloaded .rws files for aspell --extra-dicts
	MinLength           int              `yaml:"min_length"`
	NoIgnoreIdentifiers bool             `yaml:"no_ignore_identifiers"`
}

var (
	acceptableWordsGlobal = map[string]struct{}{}
	badWordsGlobal        = map[string]struct{}{}
)

func (a Aspell) checkSingle(data string, allowedWords []string) error {
	var words []string
	var badWords []string

	checkRes, err := checkWithAspellExec(data, a.ExtraDicts...)
	if checkRes != "" {
		words = strings.Split(checkRes, "\n")
	}
	if err != nil {
		return err
	}

	for _, word := range words {
		wordLower := strings.ToLower(word)
		if len(word) < a.MinLength {
			continue
		}
		if _, ok := badWordsGlobal[wordLower]; ok {
			badWords = append(badWords, wordLower)
			continue
		}
		if _, ok := acceptableWordsGlobal[wordLower]; ok {
			continue
		}
		if slices.Contains(a.AllowedWords, wordLower) || slices.Contains(allowedWords, wordLower) {
			continue
		}
		splitted := camelcase.Split(word)
		if len(splitted) < 2 {
			splitted = strings.FieldsFunc(word, func(r rune) bool {
				return r == '_' || r == '-'
			})
		}
		if len(splitted) > 1 {
			for _, s := range splitted {
				er := a.checkSingle(s, allowedWords)
				if er != nil {
					badWordsGlobal[wordLower] = struct{}{}
					badWords = append(badWords, word+":"+s)
					break
				}
			}
		} else {
			badWordsGlobal[wordLower] = struct{}{}
			badWords = append(badWords, word)
		}
	}

	if len(badWords) > 0 {
		m := map[string]struct{}{}
		for _, w := range badWords {
			m[w] = struct{}{}
		}
		badWords = []string{}
		for k := range m {
			badWords = append(badWords, k)
		}
		slices.Sort(badWords)
		return fmt.Errorf("aspell: %s", badWords)
	}
	return nil
}

func (a Aspell) Check(commits []Commit, content []map[string]string, junitSuite junit.Interface, gitHashes map[string]struct{}) error {
	preparedCommits := a.prepareCommits(commits, gitHashes)
	identifierWords := a.collectIdentifiers(content)

	var response strings.Builder
	switch a.Mode {
	case modeDisabled:
		return nil
	case modeSubject:
		a.checkSubjects(commits, junitSuite, &response)
	case modeCommit, modeAll:
		if a.Mode == modeAll {
			a.checkFiles(content, identifierWords, junitSuite, &response)
		}
		a.checkCommitMessages(preparedCommits, identifierWords, junitSuite, &response)
	}

	if len(response.String()) > 0 {
		return fmt.Errorf("%s", response.String())
	}
	return nil
}

// prepareCommits strips signature lines and known hash references from each
// commit message body, preserving the hash/subject for error reporting.
func (Aspell) prepareCommits(commits []Commit, gitHashes map[string]struct{}) []Commit {
	prepared := make([]Commit, 0, len(commits))
	for _, c := range commits {
		lines := []string{}
		for l := range strings.SplitSeq(c.Message, "\n") {
			if isSignatureLine(strings.TrimSpace(l)) {
				continue
			}
			lines = append(lines, l)
		}
		c.Message = strings.Join(lines, "\n")
		if len(gitHashes) > 0 {
			c.Message = removeKnownHashesFromBody(c.Message, gitHashes)
		}
		prepared = append(prepared, c)
	}
	return prepared
}

func isSignatureLine(line string) bool {
	prefixes := []string{
		"Signed-off-by:",
		"Reviewed-by:",
		"Tested-by:",
		"Helped-by:",
		"Reported-by:",
		"Author:",
		"Co-authored-by:",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func (a Aspell) collectIdentifiers(content []map[string]string) []string {
	if a.NoIgnoreIdentifiers {
		return nil
	}
	seen := map[string]struct{}{}
	addWords := func(name, data string) {
		for _, word := range match.GetIdentifiersFromContent(name, data) {
			if _, ok := seen[word]; !ok {
				seen[word] = struct{}{}
			}
		}
	}

	switch a.IdentifierScope {
	case identifierScopeDiff:
		for _, file := range content {
			for name, v := range file {
				addWords(name, v)
			}
		}
	case identifierScopeFiles, "":
		// Read full file content for each changed file
		for _, file := range content {
			for name := range file {
				data, err := os.ReadFile(name)
				if err != nil {
					log.Printf("aspell: could not read file %s for identifiers, using diff: %v", name, err)
					addWords(name, file[name])
					continue
				}
				addWords(name, string(data))
			}
		}
	case identifierScopeAll:
		// Collect from all files in the repo
		_ = filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
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
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			addWords(path, string(data))
			return nil
		})
	}

	identifierWords := make([]string, 0, len(seen))
	for w := range seen {
		identifierWords = append(identifierWords, w)
	}
	if len(identifierWords) > 0 {
		log.Printf("collected %d identifiers (scope: %s) for spell check filtering", len(identifierWords), a.IdentifierScope)
	}
	return identifierWords
}

func (a Aspell) checkSubjects(commits []Commit, junitSuite junit.Interface, response *strings.Builder) {
	for _, c := range commits {
		if err := a.checkSingle(c.Subject, []string{}); err != nil {
			junitSuite.AddMessageFailed("commit message", "aspell check failed", err.Error())
			log.Printf("commit %s subject %q %s", c.Hash, c.Subject, err.Error())
			_, _ = fmt.Fprintf(response, "commit %s %q: %s\n", c.Hash, c.Subject, err)
		}
	}
}

func (a Aspell) isIgnoredFile(name string) bool {
	for _, filter := range a.IgnoreFiles {
		if match.MatchFilter(name, filter) {
			return true
		}
	}
	return false
}

func (a Aspell) checkFiles(content []map[string]string, identifierWords []string, junitSuite junit.Interface, response *strings.Builder) {
	for _, file := range content {
		for name, v := range file {
			if a.isIgnoredFile(name) {
				continue
			}
			var imports []string
			if strings.HasSuffix(name, ".go") {
				imports = match.GetImportWordsFromGoFile(name)
			}
			imports = append(imports, identifierWords...)
			if err := a.checkSingle(v, imports); err != nil {
				junitSuite.AddMessageFailed(name, "aspell check failed", err.Error())
				log.Println(name, err.Error())
				_, _ = fmt.Fprintf(response, "%s\n", err)
			}
		}
	}
}

func (a Aspell) checkCommitMessages(commits []Commit, identifierWords []string, junitSuite junit.Interface, response *strings.Builder) {
	for _, c := range commits {
		parts := strings.SplitN(c.Message, "\n\n", 2)
		subject := parts[0]
		if err := a.checkSingle(subject, []string{}); err != nil {
			junitSuite.AddMessageFailed("commit message", "aspell check failed", err.Error())
			log.Printf("commit %s subject %q %s", c.Hash, subject, err.Error())
			_, _ = fmt.Fprintf(response, "commit %s %q: %s\n", c.Hash, subject, err)
		}
		if len(parts) > 1 {
			if err := a.checkSingle(parts[1], identifierWords); err != nil {
				junitSuite.AddMessageFailed("commit message", "aspell check failed", err.Error())
				log.Printf("commit %s body %q %s", c.Hash, subject, err.Error())
				_, _ = fmt.Fprintf(response, "commit %s %q (body): %s\n", c.Hash, subject, err)
			}
		}
	}
}

var hexStringRe = regexp.MustCompile(`[0-9a-fA-F]{7,40}`)

// removeKnownHashesFromBody removes known git commit hashes from the body
// of a commit message, leaving the subject line intact. A hex string in the
// body is removed if it is a prefix of (or equal to) any known full hash.
func removeKnownHashesFromBody(message string, fullHashes map[string]struct{}) string {
	parts := strings.SplitN(message, "\n\n", 2)
	if len(parts) < 2 {
		return message // no body
	}

	body := hexStringRe.ReplaceAllStringFunc(parts[1], func(match string) string {
		lower := strings.ToLower(match)
		for hash := range fullHashes {
			if strings.HasPrefix(hash, lower) {
				return ""
			}
		}
		return match
	})

	return parts[0] + "\n\n" + body
}

func checkWithAspellExec(subject string, extraDicts ...string) (string, error) {
	args := []string{"--lang=en", "--list"}
	for _, dict := range extraDicts {
		args = append(args, "--extra-dicts="+dict)
	}
	cmd := exec.Command("aspell", args...)
	cmd.Stdin = strings.NewReader(subject)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Printf("aspell error: %s, stderr: %s", err, stderr.String())
		return "", err
	}

	return stdout.String() + stderr.String(), nil
}
