package aspell

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"sort"
	"strings"

	"github.com/haproxytech/check-commit/v5/match"

	"github.com/fatih/camelcase"
)

type Aspell struct {
	Mode         mode     `yaml:"mode"`
	MinLength    int      `yaml:"min_length"`
	IgnoreFiles  []string `yaml:"ignore_files"`
	AllowedWords []string `yaml:"allowed"`
	HelpText     string   `yaml:"-"`
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
		sort.Strings(badWords)
		return fmt.Errorf("aspell: %s", badWords)
	}
	return nil
}

func (a Aspell) Check(subjects []string, commitsFull []string, content []map[string]string) error {
	var commitsFullData []string
	for _, c := range commitsFull {
		commit := []string{}
		lines := strings.Split(c, "\n")
		for _, l := range lines {
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

	var response string
	var checks []string
	switch a.Mode {
	case modeDisabled:
		return nil
	case modeSubject:
		checks = subjects
	case modeCommit:
		checks = commitsFullData
	case modeAll:
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
				if err := a.checkSingle(v, imports); err != nil {
					log.Println(name, err.Error())
					response += fmt.Sprintf("%s\n", err)
				}
			}
		}
		checks = commitsFullData
	default:
		checks = subjects
	}

	for _, subject := range checks {
		if err := a.checkSingle(subject, []string{}); err != nil {
			log.Println("commit message", err.Error())
			response += fmt.Sprintf("%s\n", err)
		}
	}

	if len(response) > 0 {
		return fmt.Errorf("%s", response)
	}
	return nil
}

func checkWithAspellExec(subject string) (string, error) {
	cmd := exec.Command("aspell", "--list")
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
