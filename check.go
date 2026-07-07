package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/go-github/v56/github"
	"github.com/haproxytech/check-commit/v5/aspell"
	"github.com/haproxytech/check-commit/v5/junit"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"golang.org/x/oauth2"
	yaml "gopkg.in/yaml.v3"
)

type patchTypeT struct {
	Scope  string   `yaml:"Scope"`
	Values []string `yaml:"Values"`
}

type tagAlternativesT struct {
	PatchTypes []string `yaml:"PatchTypes"`
	Optional   bool     `yaml:"Optional"`
}

type CommitPolicyConfig struct {
	PatchScopes map[string][]string   `yaml:"PatchScopes"`
	PatchTypes  map[string]patchTypeT `yaml:"PatchTypes"`
	HelpText    string                `yaml:"HelpText"`
	TagOrder    []tagAlternativesT    `yaml:"TagOrder"`
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

func checkSubjectText(subject string, junitSuite junit.Interface) error {
	subjectLen := utf8.RuneCountInString(subject)
	subjectParts := strings.Fields(subject)
	subjectPartsLen := len(subjectParts)

	if subject != strings.Join(subjectParts, " ") {
		junitSuite.AddMessageFailed(ErrSubjectMessageFormat.Error(), "malformatted subject string (trailing or double spaces?)", fmt.Sprintf("subject: %s", subject))
		return fmt.Errorf(
			"malformatted subject string (trailing or double spaces?): '%s' (%w)",
			subject, ErrSubjectMessageFormat,
		)
	}

	if subjectPartsLen < MINSUBJECTPARTS || subjectPartsLen > MAXSUBJECTPARTS {
		junitSuite.AddMessageFailed(
			ErrSubjectMessageFormat.Error(),
			fmt.Sprintf("subject word count out of bounds [words %d < %d < %d]", MINSUBJECTPARTS, subjectPartsLen, MAXSUBJECTPARTS),
			fmt.Sprintf("subject: %s", subject),
		)
		return fmt.Errorf(
			"subject word count out of bounds [words %d < %d < %d] '%s': %w",
			MINSUBJECTPARTS, subjectPartsLen, MAXSUBJECTPARTS, subjectParts, ErrSubjectMessageFormat,
		)
	}

	if subjectLen < MINSUBJECTLEN || subjectLen > MAXSUBJECTLEN {
		junitSuite.AddMessageFailed(
			ErrSubjectMessageFormat.Error(),
			fmt.Sprintf("subject length out of bounds [len %d < %d < %d]", MINSUBJECTLEN, subjectLen, MAXSUBJECTLEN),
			fmt.Sprintf("subject: %s", subject),
		)
		return fmt.Errorf(
			"subject length out of bounds [len %d < %d < %d] '%s': %w",
			MINSUBJECTLEN, subjectLen, MAXSUBJECTLEN, subject, ErrSubjectMessageFormat,
		)
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

			if slices.Contains(c.PatchScopes[c.PatchTypes[patchTypeName].Scope], severity) {
				tagScopeOK = true
			}
		}
	}

	return tagScopeOK
}

var ErrTagScope = errors.New("invalid tag and or severity")

func (c CommitPolicyConfig) CheckSubject(rawSubject []byte, junitSuite junit.Interface) error {
	// check for ascii-only before anything else
	for i := 0; i < len(rawSubject); i++ {
		if rawSubject[i] > unicode.MaxASCII {
			junitSuite.AddMessageFailed("", "non-ascii characters detected in commit subject", fmt.Sprintf("subject: %s", rawSubject))
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
				junitSuite.AddMessageFailed("", "invalid or missing tag/severity in commit message", fmt.Sprintf("subject: %s", rawSubject))
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
				tagOK = true

				break
			}
		}

		candidates = append(candidates, string(tagPart))

		if !tagOK {
			junitSuite.AddMessageFailed("", "invalid tag/severity in commit message", fmt.Sprintf("subject: %s", rawSubject))
			log.Printf("unable to find match in %s\n", candidates)

			return fmt.Errorf("invalid tag or no tag found, searched through [%s]: %w",
				strings.Join(tagAlternative.PatchTypes, ", "), ErrTagScope)
		}
	}

	submatch := r.FindSubmatchIndex(rawSubject)
	if len(submatch) != 0 { // no match
		junitSuite.AddMessageFailed("", "unprocessed tags detected in commit message", fmt.Sprintf("subject: %s", rawSubject))
		return fmt.Errorf("detected unprocessed tags, %w", ErrTagScope)
	}

	return checkSubjectText(string(rawSubject), junitSuite)
}

func (c CommitPolicyConfig) IsEmpty() bool {
	c1, _ := yaml.Marshal(c)
	c2, _ := yaml.Marshal(new(CommitPolicyConfig)) // empty config

	return string(c1) == string(c2)
}

var ErrGitEnvironment = errors.New("git environment error")

var (
	// errCommitDataUnavailable marks infra failures; in CI they skip checks.
	errCommitDataUnavailable = errors.New("commit data unavailable")
	// errLocalGitUnavailable marks git-clone problems; caller falls back to API.
	errLocalGitUnavailable = errors.New("local git data unavailable")
)

func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func readGitEnvironment() (string, error) {
	if os.Getenv("CHECK") == LOCAL {
		return LOCAL, nil
	}

	url := os.Getenv("GITHUB_API_URL")
	if url != "" {
		log.Printf("detected %s environment\n", GITHUB)
		log.Printf("using api url '%s'\n", url)

		return GITHUB, nil
	}

	url = os.Getenv("CI_API_V4_URL")
	if url != "" {
		log.Printf("detected %s environment\n", GITLAB)
		log.Printf("using api url '%s'\n", url)

		return GITLAB, nil
	}

	return LOCAL, nil
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

type githubEventRef struct {
	SHA string `json:"sha"`
}

type githubEventPR struct {
	Base githubEventRef `json:"base"`
	Head githubEventRef `json:"head"`
}

type githubEventPayload struct {
	PullRequest githubEventPR `json:"pull_request"`
}

// githubPRShas reads base/head SHAs from the GitHub event payload on disk.
func githubPRShas() (string, string, error) {
	if event := os.Getenv("GITHUB_EVENT_NAME"); event != "pull_request" {
		return "", "", fmt.Errorf("unsupported event name: %s", event)
	}
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return "", "", errors.New("GITHUB_EVENT_PATH not set")
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		return "", "", fmt.Errorf("reading event payload: %w", err)
	}
	var payload githubEventPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", "", fmt.Errorf("parsing event payload: %w", err)
	}
	if payload.PullRequest.Base.SHA == "" || payload.PullRequest.Head.SHA == "" {
		return "", "", errors.New("event payload has no pull_request base/head sha")
	}
	return payload.PullRequest.Base.SHA, payload.PullRequest.Head.SHA, nil
}

// gitlabMRShas reads MR base/head SHAs from GitLab CI env vars.
func gitlabMRShas() (string, string, error) {
	base := os.Getenv("CI_MERGE_REQUEST_DIFF_BASE_SHA")
	head := os.Getenv("CI_MERGE_REQUEST_SOURCE_BRANCH_SHA")
	if head == "" {
		head = os.Getenv("CI_COMMIT_SHA")
	}
	if base == "" || head == "" {
		return "", "", errors.New("CI merge request SHAs not set")
	}
	return base, head, nil
}

// resolveRange opens the repo and resolves base/head commits; failure means
// the clone is missing or too shallow and the caller should fall back.
func resolveRange(repoPath, baseSHA, headSHA string) (*object.Commit, *object.Commit, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: opening repository: %w", errLocalGitUnavailable, err)
	}
	base, err := repo.CommitObject(plumbing.NewHash(baseSHA))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: base %s not found locally: %w", errLocalGitUnavailable, baseSHA, err)
	}
	head, err := repo.CommitObject(plumbing.NewHash(headSHA))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: head %s not found locally: %w", errLocalGitUnavailable, headSHA, err)
	}
	return base, head, nil
}

// rangeData collects commits and per-commit diffs for base..head.
// Git failures wrap errLocalGitUnavailable so the caller may fall back;
// commit message validation errors are returned as-is and are fatal.
func rangeData(base, head *object.Commit, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	excluded := map[plumbing.Hash]bool{}
	baseIter := object.NewCommitPreorderIter(base, nil, nil)
	if err := baseIter.ForEach(func(c *object.Commit) error {
		excluded[c.Hash] = true
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("%w: walking base history: %w", errLocalGitUnavailable, err)
	}

	fresh := []*object.Commit{}
	if !excluded[head.Hash] {
		iter := object.NewCommitPreorderIter(head, excluded, nil)
		if err := iter.ForEach(func(c *object.Commit) error {
			fresh = append(fresh, c)
			return nil
		}); err != nil {
			return nil, nil, fmt.Errorf("%w: walking head history: %w", errLocalGitUnavailable, err)
		}
	}

	log.Printf("checking %d commit(s) in range from local git data", len(fresh))

	diffs, err := perCommitDiffs(fresh)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errLocalGitUnavailable, err)
	}

	commitData := []aspell.Commit{}
	for _, commit := range fresh {
		aspellCommit, err := toAspellCommit(commit, junitSuite)
		if err != nil {
			return nil, nil, err // policy violation, do not fall back
		}
		commitData = append(commitData, aspellCommit)
	}

	return commitData, diffs, nil
}

// gitNativeCIData tries the local-clone path shared by GitHub and GitLab.
// The bool reports whether the result (or error) is usable; false means the
// caller should fall back to the API.
func gitNativeCIData(shas func() (string, string, error), api string, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, bool, error) {
	base, head, err := shas()
	if err != nil {
		log.Printf("warning: cannot use local git data (%s), falling back to %s API", err, api)
		return nil, nil, false, nil
	}
	baseCommit, headCommit, err := resolveRange(".", base, head)
	if err != nil {
		log.Printf("warning: %s, falling back to %s API", err, api)
		return nil, nil, false, nil
	}
	commits, diffs, err := rangeData(baseCommit, headCommit, junitSuite)
	if err != nil {
		if errors.Is(err, errLocalGitUnavailable) {
			log.Printf("warning: %s, falling back to %s API", err, api)
			return nil, nil, false, nil
		}
		return nil, nil, true, err // policy violation, fatal
	}
	return commits, diffs, true, nil
}

func getGithubCommitData(junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	if commits, diffs, ok, err := gitNativeCIData(githubPRShas, GITHUB, junitSuite); ok {
		return commits, diffs, err
	}

	token := getAPIToken("GITHUB_TOKEN")
	repo := os.Getenv("GITHUB_REPOSITORY")
	ref := os.Getenv("GITHUB_REF")
	event := os.Getenv("GITHUB_EVENT_NAME")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	return githubAPICommitData(ctx, github.NewClient(tc), repo, ref, event, junitSuite)
}

func githubAPICommitData(ctx context.Context, githubClient *github.Client, repo, ref, event string, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	if event != "pull_request" {
		return nil, nil, fmt.Errorf("%w: unsupported event name: %s", errCommitDataUnavailable, event)
	}

	repoSlice := strings.SplitN(repo, "/", 2)
	if len(repoSlice) < 2 {
		return nil, nil, fmt.Errorf("%w: invalid repository format: %s", errCommitDataUnavailable, repo)
	}
	owner := repoSlice[0]
	project := repoSlice[1]

	refSlice := strings.SplitN(ref, "/", 4)
	if len(refSlice) < 3 {
		return nil, nil, fmt.Errorf("%w: invalid ref format: %s", errCommitDataUnavailable, ref)
	}
	prNo, err := strconv.Atoi(refSlice[2])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: invalid pr number %s: %w", errCommitDataUnavailable, refSlice[2], err)
	}

	commits, _, err := githubClient.PullRequests.ListCommits(ctx, owner, project, prNo, &github.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: fetching commits: %w", errCommitDataUnavailable, err)
	}

	// the PR file list is identical for every commit; fetch it once
	files, _, err := githubClient.PullRequests.ListFiles(ctx, owner, project, prNo, &github.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: fetching files: %w", errCommitDataUnavailable, err)
	}
	content := map[string]string{}
	for _, file := range files {
		if _, ok := content[file.GetFilename()]; ok {
			continue
		}
		content[file.GetFilename()] = cleanGitPatch(file.GetPatch())
	}

	commitData := []aspell.Commit{}
	for _, c := range commits {
		l := strings.SplitN(c.Commit.GetMessage(), "\n", 3)
		hash := shortHash(c.Commit.GetSHA())
		if len(l) > 1 && l[1] != "" {
			junitSuite.AddMessageFailed("", "empty line between subject and body is required", fmt.Sprintf("%s %s", hash, l[0]))
			return nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", hash, l[0])
		}
		if len(l) > 0 {
			log.Printf("detected message %s from commit %s", l[0], hash)
			commitData = append(commitData, aspell.Commit{Hash: hash, Subject: l[0], Message: c.Commit.GetMessage()})
		}
	}

	return commitData, []aspell.CommitDiff{{Files: content}}, nil
}

func getLocalCommitData(junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	repo, err := git.PlainOpen(".")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: opening local git repository: %w", errCommitDataUnavailable, err)
	}

	if published := remoteReachableHashes(repo); len(published) > 0 {
		return getFreshLocalCommitData(repo, published, junitSuite)
	}

	log.Print("no remote-tracking refs found, falling back to author-change heuristic")

	return getAuthorLocalCommitData(repo, junitSuite)
}

const upstreamRefPrefix = "refs/remotes/upstream/"

// remoteReachableHashes collects all commits reachable from remote-tracking
// refs, i.e. commits already published as of the last fetch. When an
// 'upstream' remote exists it is the sole boundary, so commits pushed only
// to a fork (origin) are still checked.
func remoteReachableHashes(repo *git.Repository) map[plumbing.Hash]bool {
	published := map[plumbing.Hash]bool{}

	refs, err := repo.References()
	if err != nil {
		return published
	}

	remoteRefs := []*plumbing.Reference{}
	hasUpstream := false
	_ = refs.ForEach(func(ref *plumbing.Reference) error {
		if !ref.Name().IsRemote() {
			return nil
		}
		if strings.HasPrefix(ref.Name().String(), upstreamRefPrefix) {
			hasUpstream = true
		}
		remoteRefs = append(remoteRefs, ref)
		return nil
	})

	if hasUpstream {
		log.Print("using 'upstream' remote as published-commit boundary")
	}

	for _, ref := range remoteRefs {
		if hasUpstream && !strings.HasPrefix(ref.Name().String(), upstreamRefPrefix) {
			continue
		}
		commit, err := repo.CommitObject(ref.Hash())
		if err != nil { // not a commit (e.g. dangling ref), skip
			continue
		}
		iter := object.NewCommitPreorderIter(commit, published, nil)
		_ = iter.ForEach(func(c *object.Commit) error {
			published[c.Hash] = true
			return nil
		})
	}

	return published
}

// getFreshLocalCommitData checks only commits reachable from HEAD but not
// from any remote-tracking ref (git rev-list HEAD --not --remotes).
func getFreshLocalCommitData(repo *git.Repository, published map[plumbing.Hash]bool, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reading git HEAD: %w", errCommitDataUnavailable, err)
	}
	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("%w: reading git HEAD commit: %w", errCommitDataUnavailable, err)
	}

	if published[headCommit.Hash] {
		log.Print("no local commits ahead of remote-tracking refs, nothing to check")
		return []aspell.Commit{}, []aspell.CommitDiff{}, nil
	}

	fresh := []*object.Commit{}
	iter := object.NewCommitPreorderIter(headCommit, published, nil)
	err = iter.ForEach(func(c *object.Commit) error {
		fresh = append(fresh, c)
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: iterating git commits: %w", errCommitDataUnavailable, err)
	}

	log.Printf("checking %d local commit(s) not present on any remote", len(fresh))

	commitData := []aspell.Commit{}
	for _, commit := range fresh {
		aspellCommit, err := toAspellCommit(commit, junitSuite)
		if err != nil {
			return nil, nil, err
		}
		commitData = append(commitData, aspellCommit)
	}

	diffs, err := perCommitDiffs(fresh)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errCommitDataUnavailable, err)
	}

	return commitData, diffs, nil
}

// getAuthorLocalCommitData collects commits from HEAD until the author name
// changes; used when no remote-tracking refs exist.
func getAuthorLocalCommitData(repo *git.Repository, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	iter, err := repo.Log(&git.LogOptions{
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: getting git log iterator: %w", errCommitDataUnavailable, err)
	}

	commitData := []aspell.Commit{}
	fresh := []*object.Commit{}
	committer := ""
	for {
		commit, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: iterating git commits: %w", errCommitDataUnavailable, err)
		}
		if committer == "" {
			committer = commit.Author.Name
		}
		if commit.Author.Name != committer {
			break
		}

		aspellCommit, err := toAspellCommit(commit, junitSuite)
		if err != nil {
			return nil, nil, err
		}
		commitData = append(commitData, aspellCommit)
		fresh = append(fresh, commit)
	}

	diffs, err := perCommitDiffs(fresh)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errCommitDataUnavailable, err)
	}

	return commitData, diffs, nil
}

func toAspellCommit(commit *object.Commit, junitSuite junit.Interface) (aspell.Commit, error) {
	l := strings.SplitN(commit.Message, "\n", 3)
	commitHash := commit.Hash.String()
	if len(commitHash) > 8 {
		commitHash = commitHash[:8]
	}
	if len(l) > 1 && l[1] != "" {
		junitSuite.AddMessageFailed("", "empty line between subject and body is required", fmt.Sprintf("%s %s", commitHash, l[0]))
		return aspell.Commit{}, fmt.Errorf("empty line between subject and body is required: %s %s", commitHash, l[0])
	}

	return aspell.Commit{Hash: commitHash, Subject: l[0], Message: commit.Message}, nil
}

// treeDiffFiles returns per-file added content between two commits.
func treeDiffFiles(from, to *object.Commit) (map[string]string, error) {
	files := map[string]string{}

	tree1, _ := to.Tree()
	tree2, _ := from.Tree()
	changes, err := object.DiffTree(tree2, tree1)
	if err != nil {
		return nil, fmt.Errorf("getting git commit changes: %w", err)
	}

	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			return nil, fmt.Errorf("getting git patch: %w", err)
		}
		for _, file := range patch.FilePatches() {
			var fileChanges strings.Builder

			for _, chunk := range file.Chunks() {
				if chunk.Type() == diff.Delete || chunk.Type() == diff.Equal {
					continue
				}
				fileChanges.WriteString(chunk.Content() + "\n")
			}
			if fileChanges.Len() == 0 {
				continue
			}

			files[change.To.Name] += fileChanges.String()
		}
	}

	return files, nil
}

// perCommitDiffs diffs each commit against its first parent for attribution.
func perCommitDiffs(commits []*object.Commit) ([]aspell.CommitDiff, error) {
	diffs := []aspell.CommitDiff{}
	for _, c := range commits {
		if c.NumParents() == 0 {
			log.Printf("skipping diff for parentless commit %s", shortHash(c.Hash.String()))
			continue
		}
		parent, err := c.Parent(0)
		if err != nil {
			return nil, fmt.Errorf("reading parent of %s: %w", shortHash(c.Hash.String()), err)
		}
		files, err := treeDiffFiles(parent, c)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			diffs = append(diffs, aspell.CommitDiff{
				Hash:    shortHash(c.Hash.String()),
				Subject: strings.SplitN(c.Message, "\n", 2)[0],
				Files:   files,
			})
		}
	}
	return diffs, nil
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

func getGitlabCommitData(junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	if commits, diffs, ok, err := gitNativeCIData(gitlabMRShas, GITLAB, junitSuite); ok {
		return commits, diffs, err
	}

	gitlabURL := os.Getenv("CI_API_V4_URL")
	token := getAPIToken("GITLAB_TOKEN")
	mri := os.Getenv("CI_MERGE_REQUEST_IID")
	project := os.Getenv("CI_MERGE_REQUEST_PROJECT_ID")

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL(gitlabURL))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: creating gitlab client: %w", errCommitDataUnavailable, err)
	}

	return gitlabAPICommitData(gitlabClient, mri, project, junitSuite)
}

func gitlabAPICommitData(gitlabClient *gitlab.Client, mri, project string, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	mrIID, err := strconv.Atoi(mri)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: invalid merge request id %s", errCommitDataUnavailable, mri)
	}

	projectID, err := strconv.Atoi(project)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: invalid project id %s", errCommitDataUnavailable, project)
	}
	commits, _, err := gitlabClient.MergeRequests.GetMergeRequestCommits(projectID, int64(mrIID), &gitlab.GetMergeRequestCommitsOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: fetching commits: %w", errCommitDataUnavailable, err)
	}

	// the MR diff list is identical for every commit; fetch it once
	mrDiffs, _, err := gitlabClient.MergeRequests.ListMergeRequestDiffs(projectID, int64(mrIID), &gitlab.ListMergeRequestDiffsOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: fetching commit changes: %w", errCommitDataUnavailable, err)
	}
	content := map[string]string{}
	for _, d := range mrDiffs {
		if _, ok := content[d.NewPath]; ok {
			continue
		}
		content[d.NewPath] = cleanGitPatch(d.Diff)
	}

	commitData := []aspell.Commit{}
	for _, c := range commits {
		l := strings.SplitN(c.Message, "\n", 3)
		hash := c.ShortID
		if len(l) == 0 {
			continue
		}
		if len(l) > 1 && l[1] != "" {
			junitSuite.AddMessageFailed("", "empty line between subject and body is required", fmt.Sprintf("%s %s", hash, l[0]))
			return nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", hash, l[0])
		}
		log.Printf("detected message %s from commit %s", l[0], hash)
		commitData = append(commitData, aspell.Commit{Hash: hash, Subject: l[0], Message: c.Message})
	}

	return commitData, []aspell.CommitDiff{{Files: content}}, nil
}

func getCommitData(repoEnv string, junitSuite junit.Interface) ([]aspell.Commit, []aspell.CommitDiff, error) {
	switch repoEnv {
	case GITHUB:
		return getGithubCommitData(junitSuite)
	case GITLAB:
		return getGitlabCommitData(junitSuite)
	case LOCAL:
		return getLocalCommitData(junitSuite)
	}
	return nil, nil, fmt.Errorf("unrecognized git environment %s", repoEnv)
}

var ErrSubjectList = errors.New("subjects contain errors")

func (c CommitPolicyConfig) CheckSubjectList(commits []aspell.Commit, junitSuite junit.Interface) error {
	hasErrors := false

	for _, commit := range commits {
		subject := strings.Trim(commit.Subject, "'")
		if err := c.CheckSubject([]byte(subject), junitSuite); err != nil {
			log.Printf("%s, commit %s original subject message '%s'", err, commit.Hash, subject)

			hasErrors = true
		}
	}

	if hasErrors {
		return ErrSubjectList
	}

	return nil
}

const requiredCmdlineArgs = 2

// getGitHashes opens the git repository at repoPath and collects all commit
// hashes (full, lowercase). These are used to ignore commit hash references
// that appear in commit message bodies.
func getGitHashes(repoPath string) map[string]struct{} {
	hashes := map[string]struct{}{}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		log.Printf("warning: could not open git repo for hash collection: %s", err)
		return hashes
	}

	iter, err := repo.Log(&git.LogOptions{
		Order: git.LogOrderCommitterTime,
		All:   true,
	})
	if err != nil {
		log.Printf("warning: could not get git log for hash collection: %s", err)
		return hashes
	}

	_ = iter.ForEach(func(c *object.Commit) error {
		full := strings.ToLower(c.Hash.String())
		hashes[full] = struct{}{}
		return nil
	})

	log.Printf("collected %d git commit hashes for body hash filtering", len(hashes))

	return hashes
}
