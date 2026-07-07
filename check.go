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

func getGithubCommitData(junitSuite junit.Interface) ([]aspell.Commit, []map[string]string, error) {
	token := getAPIToken("GITHUB_TOKEN")
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
			junitSuite.AddMessageFailed("", "error fetching owner and project from repo", fmt.Sprintf("invalid repository format: %s", repo))
			return nil, nil, fmt.Errorf("error fetching owner and project from repo %s", repo)
		}
		owner := repoSlice[0]
		project := repoSlice[1]

		refSlice := strings.SplitN(ref, "/", 4)
		if len(refSlice) < 3 {
			junitSuite.AddMessageFailed("", "error fetching PR number from ref", fmt.Sprintf("invalid ref format: %s", ref))
			return nil, nil, fmt.Errorf("error fetching PR from ref %s", ref)
		}
		prNo, err := strconv.Atoi(refSlice[2])
		if err != nil {
			junitSuite.AddMessageFailed("", "error fetching PR number from ref", fmt.Sprintf("invalid pr number: %s", refSlice[2]))
			return nil, nil, fmt.Errorf("error fetching PR number from %s: %w", refSlice[2], err)
		}

		commits, _, err := githubClient.PullRequests.ListCommits(ctx, owner, project, prNo, &github.ListOptions{})
		if err != nil {
			junitSuite.AddMessageFailed("", "error fetching commits", err.Error())
			return nil, nil, fmt.Errorf("error fetching commits: %w", err)
		}

		commitData := []aspell.Commit{}
		diffs := []map[string]string{}
		for _, c := range commits {
			l := strings.SplitN(c.Commit.GetMessage(), "\n", 3)
			hash := c.Commit.GetSHA()
			if len(hash) > 8 {
				hash = hash[:8]
			}
			if len(l) > 1 {
				if l[1] != "" {
					junitSuite.AddMessageFailed("", "empty line between subject and body is required", fmt.Sprintf("%s %s", hash, l[0]))
					return nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", hash, l[0])
				}
			}
			if len(l) > 0 {
				log.Printf("detected message %s from commit %s", l[0], hash)
				commitData = append(commitData, aspell.Commit{Hash: hash, Subject: l[0], Message: c.Commit.GetMessage()})
			}

			files, _, err := githubClient.PullRequests.ListFiles(ctx, owner, project, prNo, &github.ListOptions{})
			if err != nil {
				junitSuite.AddMessageFailed("", "error fetching files", err.Error())
				return nil, nil, fmt.Errorf("error fetching files: %w", err)
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
		return commitData, diffs, nil
	}

	junitSuite.AddMessageFailed("", "unsupported event name", fmt.Sprintf("unsupported event name: %s", event))
	return nil, nil, fmt.Errorf("unsupported event name: %s", event)
}

func getLocalCommitData(junitSuite junit.Interface) ([]aspell.Commit, []map[string]string, error) {
	repo, err := git.PlainOpen(".")
	if err != nil {
		junitSuite.AddMessageFailed("", "error opening local git repository", err.Error())
		return nil, nil, err
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
func getFreshLocalCommitData(repo *git.Repository, published map[plumbing.Hash]bool, junitSuite junit.Interface) ([]aspell.Commit, []map[string]string, error) {
	head, err := repo.Head()
	if err != nil {
		junitSuite.AddMessageFailed("", "error reading git HEAD", err.Error())
		return nil, nil, err
	}
	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		junitSuite.AddMessageFailed("", "error reading git HEAD commit", err.Error())
		return nil, nil, err
	}

	if published[headCommit.Hash] {
		log.Print("no local commits ahead of remote-tracking refs, nothing to check")
		return []aspell.Commit{}, []map[string]string{}, nil
	}

	fresh := []*object.Commit{}
	iter := object.NewCommitPreorderIter(headCommit, published, nil)
	err = iter.ForEach(func(c *object.Commit) error {
		fresh = append(fresh, c)
		return nil
	})
	if err != nil {
		junitSuite.AddMessageFailed("", "error iterating through git commits", err.Error())
		return nil, nil, err
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

	// diff base: first published parent of a fresh commit
	var base *object.Commit
	for _, c := range fresh {
		for _, p := range c.ParentHashes {
			if published[p] {
				base, err = repo.CommitObject(p)
				if err != nil {
					junitSuite.AddMessageFailed("", "error reading git commit", err.Error())
					return nil, nil, err
				}
				break
			}
		}
		if base != nil {
			break
		}
	}
	if base == nil { // no published ancestor, diff against oldest fresh commit
		base = fresh[len(fresh)-1]
	}

	diffs, err := localTreeDiffs(base, headCommit, junitSuite)
	if err != nil {
		return nil, nil, err
	}

	return commitData, diffs, nil
}

// getAuthorLocalCommitData collects commits from HEAD until the author name
// changes; used when no remote-tracking refs exist.
func getAuthorLocalCommitData(repo *git.Repository, junitSuite junit.Interface) ([]aspell.Commit, []map[string]string, error) {
	iter, err := repo.Log(&git.LogOptions{
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		junitSuite.AddMessageFailed("", "error getting git log iterator", err.Error())
		return nil, nil, err
	}

	commitData := []aspell.Commit{}
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
			junitSuite.AddMessageFailed("", "error iterating through git commits", err.Error())
			return nil, nil, err
		}
		if committer == "" {
			committer = commit.Author.Name
			commit1 = commit
		}

		if commit.Author.Name != committer {
			commit2 = commit
			break
		}

		aspellCommit, err := toAspellCommit(commit, junitSuite)
		if err != nil {
			return nil, nil, err
		}
		commitData = append(commitData, aspellCommit)
	}

	if commit2 == nil {
		commit2 = oldestCommit
	}

	diffs, err := localTreeDiffs(commit2, commit1, junitSuite)
	if err != nil {
		return nil, nil, err
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

// localTreeDiffs returns per-file added content between two commits.
func localTreeDiffs(from, to *object.Commit, junitSuite junit.Interface) ([]map[string]string, error) {
	diffs := []map[string]string{}

	tree1, _ := to.Tree()
	tree2, _ := from.Tree()
	changes, err := object.DiffTree(tree2, tree1)
	if err != nil {
		junitSuite.AddMessageFailed("", "error getting git commit changes", err.Error())
		return nil, err
	}

	for _, change := range changes {
		patch, err := change.Patch()
		if err != nil {
			junitSuite.AddMessageFailed("", "error getting git patch", err.Error())
			return nil, err
		}
		for _, file := range patch.FilePatches() {
			chunks := file.Chunks()
			var fileChanges strings.Builder

			for _, chunk := range chunks {
				if chunk.Type() == diff.Delete {
					continue
				}
				if chunk.Type() == diff.Equal {
					continue
				}
				fileChanges.WriteString(chunk.Content() + "\n")
			}
			if fileChanges.String() == "" {
				continue
			}

			diffs = append(diffs, map[string]string{change.To.Name: fileChanges.String()})
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

func getGitlabCommitData(junitSuite junit.Interface) ([]aspell.Commit, []map[string]string, error) {
	gitlabURL := os.Getenv("CI_API_V4_URL")
	token := getAPIToken("GITLAB_TOKEN")
	mri := os.Getenv("CI_MERGE_REQUEST_IID")
	project := os.Getenv("CI_MERGE_REQUEST_PROJECT_ID")

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL(gitlabURL))
	if err != nil {
		junitSuite.AddMessageFailed("", "failed to create gitlab client", err.Error())
		return nil, nil, fmt.Errorf("failed to create gitlab client: %w", err)
	}

	mrIID, err := strconv.Atoi(mri)
	if err != nil {
		junitSuite.AddMessageFailed("", "invalid merge request id", err.Error())
		return nil, nil, fmt.Errorf("invalid merge request id %s", mri)
	}

	projectID, err := strconv.Atoi(project)
	if err != nil {
		junitSuite.AddMessageFailed("", "invalid project id", err.Error())
		return nil, nil, fmt.Errorf("invalid project id %s", project)
	}
	commits, _, err := gitlabClient.MergeRequests.GetMergeRequestCommits(projectID, int64(mrIID), &gitlab.GetMergeRequestCommitsOptions{})
	if err != nil {
		junitSuite.AddMessageFailed("", "error fetching commits", err.Error())
		return nil, nil, fmt.Errorf("error fetching commits: %w", err)
	}

	commitData := []aspell.Commit{}
	diffs := []map[string]string{}
	for _, c := range commits {
		l := strings.SplitN(c.Message, "\n", 3)
		hash := c.ShortID
		if len(l) > 0 {
			if len(l) > 1 {
				if l[1] != "" {
					junitSuite.AddMessageFailed("", "empty line between subject and body is required", fmt.Sprintf("%s %s", hash, l[0]))
					return nil, nil, fmt.Errorf("empty line between subject and body is required: %s %s", hash, l[0])
				}
			}
			log.Printf("detected message %s from commit %s", l[0], hash)
			commitData = append(commitData, aspell.Commit{Hash: hash, Subject: l[0], Message: c.Message})
			mrDiffs, _, err := gitlabClient.MergeRequests.ListMergeRequestDiffs(projectID, int64(mrIID), &gitlab.ListMergeRequestDiffsOptions{})
			if err != nil {
				junitSuite.AddMessageFailed("", "error fetching commit changes", err.Error())
				return nil, nil, fmt.Errorf("error fetching commit changes: %w", err)
			}
			content := map[string]string{}
			for _, d := range mrDiffs {
				if _, ok := content[d.NewPath]; ok {
					continue
				}
				content[d.NewPath] = cleanGitPatch(d.Diff)
			}
			diffs = append(diffs, content)
		}
	}

	return commitData, diffs, nil
}

func getCommitData(repoEnv string, junitSuite junit.Interface) ([]aspell.Commit, []map[string]string, error) {
	if repoEnv == GITHUB {
		return getGithubCommitData(junitSuite)
	} else if repoEnv == GITLAB {
		return getGitlabCommitData(junitSuite)
	} else if repoEnv == LOCAL {
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
