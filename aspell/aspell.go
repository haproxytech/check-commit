package aspell

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/haproxytech/check-commit/v5/junit"
	"github.com/haproxytech/check-commit/v5/match"

	"github.com/fatih/camelcase"
)

type RemoteFile struct {
	URL             string `yaml:"url"`
	URLEnv          string `yaml:"url_env"`
	HeaderFromENV   string `yaml:"header_from_env"`
	PrivateTokenENV string `yaml:"private_token_env"`
	AllowedItemsKey string `yaml:"allowed_items_key"`
}

type Aspell struct {
	RemoteFile        RemoteFile `yaml:"remote_file"`
	Mode              mode       `yaml:"mode"`
	HelpText          string     `yaml:"-"`
	IgnoreFiles       []string   `yaml:"ignore_files"`
	AllowedWords      []string   `yaml:"allowed"`
	MinLength         int        `yaml:"min_length"`
	NoIgnoreIdentifiers bool     `yaml:"no_ignore_identifiers"`
}

var (
	acceptableWordsGlobal = map[string]struct{}{
		"haproxy":    {},
		"golang":     {},
		"ascii":      {},
		"api":        {},
		"goreleaser": {},
		"github":     {},
		"gitlab":     {},
		"yaml":       {},
		"env":        {},
		"config":     {},
		"workdir":    {},
		"entrypoint": {},
		"sudo":       {},
		"dockerfile": {},
		"ghcr":       {},
		"sed":        {},
		"stdin":      {},
		"args":       {},
		"arg":        {},
		"dev":        {},
		"vcs":        {},
	}
	badWordsGlobal = map[string]struct{}{}
)

func (a Aspell) checkSingle(data string, allowedWords []string) error {
	var words []string
	var badWords []string

	checkRes, err := checkWithAspellExec(data)
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

func (a Aspell) Check(subjects []string, commitsFull []string, content []map[string]string, junitSuite junit.Interface, gitHashes map[string]struct{}) error {
	var commitsFullData []string
	for _, c := range commitsFull {
		commit := []string{}
		lines := strings.SplitSeq(c, "\n")
		for l := range lines {
			c2 := strings.TrimSpace(l)
			if strings.HasPrefix(c2, "Signed-off-by:") ||
				strings.HasPrefix(c2, "Reviewed-by:") ||
				strings.HasPrefix(c2, "Tested-by:") ||
				strings.HasPrefix(c2, "Helped-by:") ||
				strings.HasPrefix(c2, "Reported-by:") ||
				strings.HasPrefix(c2, "Author:") ||
				strings.HasPrefix(c2, "Co-authored-by:") {
				continue
			}

			commit = append(commit, l)
		}
		commitsFullData = append(commitsFullData, strings.Join(commit, "\n"))
	}

	// Remove known git commit hashes from body portions of commit messages
	// so they are not flagged by spell checking. Subject lines are preserved.
	if len(gitHashes) > 0 {
		for i, c := range commitsFullData {
			commitsFullData[i] = removeKnownHashesFromBody(c, gitHashes)
		}
	}

	// Collect identifiers (function names, variable names, etc.) from diff
	// content so they can be ignored during spell checking.
	var identifierWords []string
	if !a.NoIgnoreIdentifiers {
		seen := map[string]struct{}{}
		for _, file := range content {
			for name, v := range file {
				for _, word := range match.GetIdentifiersFromContent(name, v) {
					if _, ok := seen[word]; !ok {
						seen[word] = struct{}{}
						identifierWords = append(identifierWords, word)
					}
				}
			}
		}
		if len(identifierWords) > 0 {
			log.Printf("collected %d identifiers from diff content for spell check filtering", len(identifierWords))
		}
	}

	var response strings.Builder
	switch a.Mode {
	case modeDisabled:
		return nil
	case modeSubject:
		for _, subject := range subjects {
			if err := a.checkSingle(subject, []string{}); err != nil {
				junitSuite.AddMessageFailed("commit message", "aspell check failed", err.Error())
				log.Println("commit message", err.Error())
				response.WriteString(fmt.Sprintf("%s\n", err))
			}
		}
	case modeCommit, modeAll:
		if a.Mode == modeAll {
			for _, file := range content {
				for name, v := range file {
					nextFile := false
					for _, filter := range a.IgnoreFiles {
						if match.MatchFilter(name, filter) {
							// log.Println("File", name, "in ignore list")
							nextFile = true
							continue
						}
					}
					if nextFile {
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
						response.WriteString(fmt.Sprintf("%s\n", err))
					}
				}
			}
		}
		// Check commit messages: subject without identifiers, body with identifiers
		for _, msg := range commitsFullData {
			parts := strings.SplitN(msg, "\n\n", 2)
			// Subject — no identifier filtering (same as hash behavior)
			if err := a.checkSingle(parts[0], []string{}); err != nil {
				junitSuite.AddMessageFailed("commit message", "aspell check failed", err.Error())
				log.Println("commit message", err.Error())
				response.WriteString(fmt.Sprintf("%s\n", err))
			}
			// Body — identifier filtering allowed
			if len(parts) > 1 {
				if err := a.checkSingle(parts[1], identifierWords); err != nil {
					junitSuite.AddMessageFailed("commit message", "aspell check failed", err.Error())
					log.Println("commit message body", err.Error())
					response.WriteString(fmt.Sprintf("%s\n", err))
				}
			}
		}
	}

	if len(response.String()) > 0 {
		return fmt.Errorf("%s", response.String())
	}
	return nil
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

func checkWithAspellExec(subject string) (string, error) {
	cmd := exec.Command("aspell", "--lang=en", "--list")
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
