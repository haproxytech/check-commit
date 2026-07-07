package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/haproxytech/check-commit/v5/junit"
)

func TestPerCommitAttribution(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	base := commitFile(t, wt, dir, "base.txt", "base", "MINOR: base: some base commit message", "alice")
	first := commitFile(t, wt, dir, "one.txt", "content one", "MINOR: one: first fresh commit", "alice")
	second := commitFile(t, wt, dir, "two.txt", "content two", "MINOR: two: second fresh commit", "alice")

	baseCommit, err := repo.CommitObject(base)
	if err != nil {
		t.Fatal(err)
	}
	headCommit, err := repo.CommitObject(second)
	if err != nil {
		t.Fatal(err)
	}

	commits, diffs, err := rangeData(baseCommit, headCommit, &junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d: %+v", len(commits), commits)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 attributed diffs, got %d: %+v", len(diffs), diffs)
	}

	byHash := map[string]map[string]string{}
	for _, d := range diffs {
		byHash[d.Hash] = d.Files
	}
	if _, ok := byHash[shortHash(first.String())]["one.txt"]; !ok {
		t.Errorf("one.txt should be attributed to %s, got %v", shortHash(first.String()), byHash)
	}
	if _, ok := byHash[shortHash(second.String())]["two.txt"]; !ok {
		t.Errorf("two.txt should be attributed to %s, got %v", shortHash(second.String()), byHash)
	}
	if _, ok := byHash[shortHash(second.String())]["one.txt"]; ok {
		t.Error("one.txt must not be attributed to the second commit")
	}
}

func TestResolveRangeUnknownShaFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	head := commitFile(t, wt, dir, "a.txt", "a", "MINOR: base: some base commit message", "alice")

	// base sha not present locally, e.g. shallow clone
	_, _, err = resolveRange(".", strings.Repeat("1", 40), head.String())
	if !errors.Is(err, errLocalGitUnavailable) {
		t.Fatalf("expected errLocalGitUnavailable, got %v", err)
	}
}

func TestGithubCommitDataUsesLocalGit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	base := commitFile(t, wt, dir, "base.txt", "base", "MINOR: base: some base commit message", "alice")
	head := commitFile(t, wt, dir, "one.txt", "content", "MINOR: one: first fresh commit", "alice")

	payload := fmt.Sprintf(`{"pull_request":{"base":{"sha":"%s"},"head":{"sha":"%s"}}}`, base, head)
	eventPath := filepath.Join(dir, "event.json")
	if err := os.WriteFile(eventPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)

	// no API server is reachable: success proves the local git path was used
	commits, diffs, err := getGithubCommitData(&junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Subject != "MINOR: one: first fresh commit" {
		t.Fatalf("unexpected commits: %+v", commits)
	}
	if len(diffs) != 1 || diffs[0].Hash != shortHash(head.String()) {
		t.Fatalf("unexpected diffs: %+v", diffs)
	}
	if diffs[0].Subject != "MINOR: one: first fresh commit" {
		t.Fatalf("diff subject = %q, want commit subject", diffs[0].Subject)
	}
}

func TestGitlabCommitDataUsesLocalGit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	base := commitFile(t, wt, dir, "base.txt", "base", "MINOR: base: some base commit message", "alice")
	head := commitFile(t, wt, dir, "one.txt", "content", "MINOR: one: first fresh commit", "alice")

	t.Setenv("CI_MERGE_REQUEST_DIFF_BASE_SHA", base.String())
	t.Setenv("CI_COMMIT_SHA", head.String())

	commits, diffs, err := getGitlabCommitData(&junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Subject != "MINOR: one: first fresh commit" {
		t.Fatalf("unexpected commits: %+v", commits)
	}
	if len(diffs) != 1 || diffs[0].Hash != shortHash(head.String()) {
		t.Fatalf("unexpected diffs: %+v", diffs)
	}
}

func TestRangeDataMessageValidationIsFatal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	base := commitFile(t, wt, dir, "base.txt", "base", "MINOR: base: some base commit message", "alice")
	head := commitFile(t, wt, dir, "one.txt", "content", "MINOR: one: subject\nbody without empty line", "alice")

	baseCommit, _ := repo.CommitObject(base)
	headCommit, _ := repo.CommitObject(head)

	_, _, err = rangeData(baseCommit, headCommit, &junit.JunitSuiteDummy{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if errors.Is(err, errLocalGitUnavailable) {
		t.Fatalf("validation error must not trigger API fallback: %v", err)
	}
}
