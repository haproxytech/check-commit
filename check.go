package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-github/v56/github"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"golang.org/x/oauth2"
	yaml "gopkg.in/yaml.v3"
)

type patchTypeT struct {
	Values []string `yaml:"Values"`
	Scope  string   `yaml:"Scope"`
}

type tagAlternativesT struct {
	PatchTypes []string `yaml:"PatchTypes"`
	Optional   bool     `yaml:"Optional"`
}

type CommitPolicyConfig struct {
	PatchScopes map[string][]string   `yaml:"PatchScopes"`
	PatchTypes  map[string]patchTypeT `yaml:"PatchTypes"`
	TagOrder    []tagAlternativesT    `yaml:"TagOrder"`
	HelpText    string                `yaml:"HelpText"`
}

const (
	defaultConfig = `
---
HelpText: "Please refer to https://github.com/haproxy/haproxy/blob/master/CONTRIBUTING#L632"
PatchScopes:
  HAProxy Standard Scope:
    - MINOR
    - MEDIUM
    - MAJOR
    - CRITICAL
PatchTypes:
  HAProxy Standard Patch:
    Values:
      - BUG
      - BUILD
      - CLEANUP
      - DOC
      - LICENSE
      - OPTIM
      - RELEASE
      - REORG
      - TEST
      - REVERT
    Scope: HAProxy Standard Scope
  HAProxy Standard Feature Commit:
    Values:
      - MINOR
      - MEDIUM
      - MAJOR
      - CRITICAL
TagOrder:
  - PatchTypes:
    - HAProxy Standard Patch
    - HAProxy Standard Feature Commit
`

	MINSUBJECTPARTS = 3
	MAXSUBJECTPARTS = 15
	MINSUBJECTLEN   = 15
	MAXSUBJECTLEN   = 100

	GITHUB = "Github"
	GITLAB = "Gitlab"
	LOCAL  = "local"
)

var ErrSubjectMessageFormat = errors.New("invalid subject message format")

func checkSubjectText(subject string) error {
	subjectLen := utf8.RuneCountInString(subject)
	subjectParts := strings.Fields(subject)
	subjectPartsLen := len(subjectParts)

	if subject != strings.Join(subjectParts, " ") {
		return fmt.Errorf(
			"malformatted subject string (trailing or double spaces?): '%s' (%w)",
			subject, ErrSubjectMessageFormat)
	}

	if subjectPartsLen < MINSUBJECTPARTS || subjectPartsLen > MAXSUBJECTPARTS {
		return fmt.Errorf(
			"subject word count out of bounds [words %d < %d < %d] '%s': %w",
			MINSUBJECTPARTS, subjectPartsLen, MAXSUBJECTPARTS, subjectParts, ErrSubjectMessageFormat)
	}

	if subjectLen < MINSUBJECTLEN || subjectLen > MAXSUBJECTLEN {
		return fmt.Errorf(
			"subject length out of bounds [len %d < %d < %d] '%s': %w",
			MINSUBJECTLEN, subjectLen, MAXSUBJECTLEN, subject, ErrSubjectMessageFormat)
	}

	return nil
}

func (c CommitPolicyConfig) CheckPatchTypes(tag, severity string, patchTypeName string) bool {
	tagScopeOK := false

	for _, allowedTag := range c.PatchTypes[patchTypeName].Values {
		if tag == allowedTag {
			if severity == "" {
				tagScopeOK = true

				break
			}

			if c.PatchTypes[patchTypeName].Scope == "" {
				log.Printf("unable to verify severity %s without definitions", severity)

				break // subject has severity but there is no definition to verify it
			}

			for _, allowedScope := range c.PatchScopes[c.PatchTypes[patchTypeName].Scope] {
				if severity == allowedScope {
					tagScopeOK = true

					break
				}
			}
		}
	}

	return tagScopeOK
}

var ErrTagScope = errors.New("invalid tag and or severity")

func (c CommitPolicyConfig) CheckSubject(rawSubject []byte) error {
	// check for ascii-only before anything else
	for i := 0; i < len(rawSubject); i++ {
		if rawSubject[i] > unicode.MaxASCII {
			log.Printf("non-ascii characters detected in in subject:\n%s", hex.Dump(rawSubject))

			return fmt.Errorf("non-ascii characters in commit subject: %w", ErrTagScope)
		}
	}
	// 5 subgroups, 4. is "/severity", 5. is "severity"
	r := regexp.MustCompile(`^(?P<match>(?P<tag>[A-Z]+)(\/(?P<severity>[A-Z]+))?: )`)

	tTag := []byte("$tag")
	tScope := []byte("$severity")
	result := []byte{}

	candidates := []string{}

	var tag, severity string

	for _, tagAlternative := range c.TagOrder {
		tagOK := tagAlternative.Optional

		submatch := r.FindSubmatchIndex(rawSubject)
		if len(submatch) == 0 { // no match
			if !tagOK {
				log.Printf("unable to find match in %s\n", rawSubject)

				return fmt.Errorf("invalid tag or no tag found, searched through [%s]: %w",
					strings.Join(tagAlternative.PatchTypes, ", "), ErrTagScope)
			}
			continue
		}

		tagPart := rawSubject[submatch[0]:submatch[1]]

		tag = string(r.Expand(result, tTag, tagPart, submatch))
		severity = string(r.Expand(result, tScope, tagPart, submatch))

		for _, pType := range tagAlternative.PatchTypes { // we allow more than one set of tags in a position
			if c.CheckPatchTypes(tag, severity, pType) { // we found what we were looking for, so consume input
				rawSubject = rawSubject[submatch[1]:]
				tagOK = tagOK || true

				break
			}
		}

		candidates = append(candidates, string(tagPart))

		if !tagOK {
			log.Printf("unable to find match in %s\n", candidates)

			return fmt.Errorf("invalid tag or no tag found, searched through [%s]: %w",
				strings.Join(tagAlternative.PatchTypes, ", "), ErrTagScope)
		}
	}

	submatch := r.FindSubmatchIndex(rawSubject)
	if len(submatch) != 0 { // no match
		return fmt.Errorf("detected unprocessed tags, %w", ErrTagScope)
	}

	return checkSubjectText(string(rawSubject))
}

func (c CommitPolicyConfig) IsEmpty() bool {
	c1, _ := yaml.Marshal(c)
	c2, _ := yaml.Marshal(new(CommitPolicyConfig)) // empty config

	return string(c1) == string(c2)
}

var ErrGitEnvironment = errors.New("git environment error")

func readGitEnvironment() (string, error) {
	if os.Getenv("CHECK") == LOCAL {
		return LOCAL, nil
	}

	url := os.Getenv("GITHUB_API_URL")
	if url != "" {
		log.Printf("detected %s environment\n", GITHUB)
		log.Printf("using api url '%s'\n", url)

		return GITHUB, nil
	} else {
		url = os.Getenv("CI_API_V4_URL")
		if url != "" {
			log.Printf("detected %s environment\n", GITLAB)
			log.Printf("using api url '%s'\n", url)

			return GITLAB, nil
		} else {
			return LOCAL, nil
			// return "", fmt.Errorf("no suitable git environment variables found: %w", ErrGitEnvironment)
		}
	}
}

func LoadCommitPolicy(filename string) (CommitPolicyConfig, error) {
	var commitPolicy CommitPolicyConfig

	var config string

	if data, err := os.ReadFile(filename); err != nil {
		log.Printf("warning: using built-in fallback configuration with HAProxy defaults (%s)", err)

		config = defaultConfig
	} else {
		config = string(data)
	}

	if err := yaml.Unmarshal([]byte(config), &commitPolicy); err != nil {
		return CommitPolicyConfig{}, fmt.Errorf("error loading commit policy: %w", err)
	}

	return commitPolicy, nil
}

func getGithubCommitData() ([]string, []string, []map[string]string, error) {
	token := os.Getenv("API_TOKEN")
	repo := os.Getenv("GITHUB_REPOSITORY")
	ref := os.Getenv("GITHUB_REF")
	event := os.Getenv("GITHUB_EVENT_NAME")

	ctx := context.Background()

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	githubClient := github.NewClient(tc)

	if event == "pull_request" {
		repoSlice := strings.SplitN(repo, "/", 2)
		if len(repoSlice) < 2 {
			return nil, nil, nil, fmt.Errorf("error fetching owner and project from repo %s", repo)
		}
		owner := repoSlice[0]
		project := repoSlice[1]

		refSlice := strings.SplitN(ref, "/", 4)
		if len(refSlice) < 3 {
			return nil, nil, nil, fmt.Errorf("error fetching pr from ref %s", ref)
		}
		prNo, err := strconv.Atoi(refSlice[2])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("Error fetching pr number from %s: %w", refSlice[2], err)
		}

		commits, _, err := githubClient.PullRequests.ListCommits(ctx, owner, project, prNo, &github.ListOptions{})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error fetching commits: %w", err)
		}

		subjects := []string{}
		messages := []string{}
		diffs := []map[string]string{}
		for _, c := range commits {
			l := strings.SplitN(c.Commit.GetMessage(), "\n", 3)
			hash := c.Commit.GetSHA()
			if len(hash) > 8 {
				hash = hash[:8]
			}
			if len(l) > 1 {
				if l[1] != "" {
					return nil, nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", hash, l[0])
				}
			}
			if len(l) > 0 {
				log.Printf("detected message %s from commit %s", l[0], hash)
				subjects = append(subjects, l[0])
				messages = append(messages, c.Commit.GetMessage())
			}

			files, _, err := githubClient.PullRequests.ListFiles(ctx, owner, project, prNo, &github.ListOptions{})
			if err != nil {
				return nil, nil, nil, fmt.Errorf("error fetching files: %w", err)
			}
			content := map[string]string{}
			for _, file := range files {
				if _, ok := content[file.GetFilename()]; ok {
					continue
				}
				content[file.GetFilename()] = cleanGitPatch(file.GetPatch())
			}
			diffs = append(diffs, content)
		}
		return subjects, messages, diffs, nil
	} else {
		return nil, nil, nil, fmt.Errorf("unsupported event name: %s", event)
	}
}

func getLocalCommitData() ([]string, []string, []map[string]string, error) {
	repo, err := git.PlainOpen(".")
	if err != nil {
		return nil, nil, nil, err
	}

	iter, err := repo.Log(&git.LogOptions{
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	subjects := []string{}
	messages := []string{}
	diffs := []map[string]string{}
	committer := ""
	var commit1 *object.Commit
	var oldestCommit *object.Commit
	var commit2 *object.Commit
	for {
		commit, err := iter.Next()
		if commit != nil {
			oldestCommit = commit
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, nil, err
		}
		if committer == "" {
			committer = commit.Author.Name
			commit1 = commit
		}

		if commit.Author.Name != committer {
			commit2 = commit
			break
		}

		commitBody := commit.Message
		l := strings.SplitN(string(commitBody), "\n", 3)
		commitHash := commit.Hash.String()
		if len(commitHash) > 8 {
			commitHash = commitHash[:8]
		}
		if len(l) > 1 {
			if l[1] != "" {
				return nil, nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", commitHash, l[0])
			}
		}
		if len(l) > 0 {
			subjects = append(subjects, l[0])
			messages = append(messages, string(commitBody))
		}
	}

	// Get the changes (diff) between the two commits
	tree1, _ := commit1.Tree()
	if commit2 == nil {
		commit2 = oldestCommit
	}
	tree2, _ := commit2.Tree()
	changes, err := object.DiffTree(tree2, tree1)
	if err != nil {
		return nil, nil, nil, err
	}

	// Print the list of changed files and their content (patch)
	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			return nil, nil, nil, err
		}
		for _, file := range patch.FilePatches() {
			chunks := file.Chunks()
			fileChanges := ``

			for _, chunk := range chunks {
				if chunk.Type() == diff.Delete {
					continue
				}
				if chunk.Type() == diff.Equal {
					continue
				}
				fileChanges += chunk.Content() + "\n"
			}
			if fileChanges == "" {
				continue
			}

			diffs = append(diffs, map[string]string{change.To.Name: fileChanges})
		}
	}
	return subjects, messages, diffs, nil
}

func cleanGitPatch(patch string) string {
	var cleanedPatch strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(patch))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+") {
			cleanedPatch.WriteString(line)
			cleanedPatch.WriteString("\n")
		}
	}
	patch = cleanedPatch.String()
	return patch
}

func getGitlabCommitData() ([]string, []string, []map[string]string, error) {
	gitlab_url := os.Getenv("CI_API_V4_URL")
	token := os.Getenv("API_TOKEN")
	mri := os.Getenv("CI_MERGE_REQUEST_IID")
	project := os.Getenv("CI_MERGE_REQUEST_PROJECT_ID")

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL(gitlab_url))
	if err != nil {
		log.Fatalf("Failed to create gitlab client: %v", err)
	}

	mrIID, err := strconv.Atoi(mri)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid merge request id %s", mri)
	}

	projectID, err := strconv.Atoi(project)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid project id %s", project)
	}
	commits, _, err := gitlabClient.MergeRequests.GetMergeRequestCommits(projectID, mrIID, &gitlab.GetMergeRequestCommitsOptions{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error fetching commits: %w", err)
	}

	subjects := []string{}
	messages := []string{}
	diffs := []map[string]string{}
	for _, c := range commits {
		l := strings.SplitN(c.Message, "\n", 3)
		hash := c.ShortID
		if len(l) > 0 {
			if len(l) > 1 {
				if l[1] != "" {
					return nil, nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", hash, l[0])
				}
			}
			log.Printf("detected message %s from commit %s", l[0], hash)
			subjects = append(subjects, l[0])
			messages = append(messages, c.Message)
			diff, _, err := gitlabClient.MergeRequests.ListMergeRequestDiffs(projectID, mrIID, &gitlab.ListMergeRequestDiffsOptions{})
			if err != nil {
				return nil, nil, nil, fmt.Errorf("error fetching commit changes: %w", err)
			}
			content := map[string]string{}
			for _, d := range diff {
				if _, ok := content[d.NewPath]; ok {
					continue
				}
				content[d.NewPath] = cleanGitPatch(d.Diff)
			}
			diffs = append(diffs, content)
		}
	}

	return subjects, messages, diffs, nil
}

func getCommitData(repoEnv string) ([]string, []string, []map[string]string, error) {
	if repoEnv == GITHUB {
		return getGithubCommitData()
	} else if repoEnv == GITLAB {
		return getGitlabCommitData()
	} else if repoEnv == LOCAL {
		return getLocalCommitData()
	}
	return nil, nil, nil, fmt.Errorf("unrecognized git environment %s", repoEnv)
}

var ErrSubjectList = errors.New("subjects contain errors")

func (c CommitPolicyConfig) CheckSubjectList(subjects []string) error {
	errors := false

	for _, subject := range subjects {
		subject = strings.Trim(subject, "'")
		if err := c.CheckSubject([]byte(subject)); err != nil {
			log.Printf("%s, original subject message '%s'", err, subject)

			errors = true
		}
	}

	if errors {
		return ErrSubjectList
	}

	return nil
}

const requiredCmdlineArgs = 2
