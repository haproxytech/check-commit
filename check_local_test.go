package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/haproxytech/check-commit/v5/junit"
)

func commitFile(t *testing.T, wt *git.Worktree, dir, name, content, message, author string) plumbing.Hash {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatal(err)
	}
	hash, err := wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  author,
			Email: author + "@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return hash
}

func TestGetLocalCommitDataOnlyFreshCommits(t *testing.T) {
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

	commitFile(t, wt, dir, "a.txt", "a", "MINOR: base: some base commit message", "alice")
	base := commitFile(t, wt, dir, "b.txt", "b", "MINOR: base: another published commit", "alice")

	// mark everything up to here as published on origin
	err = repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/origin/main", base))
	if err != nil {
		t.Fatal(err)
	}

	// fresh commits by different authors: author-change heuristic would stop here
	commitFile(t, wt, dir, "c.txt", "c", "MINOR: one: first fresh commit", "alice")
	commitFile(t, wt, dir, "d.txt", "d", "MINOR: two: second fresh commit", "bob")

	commits, _, err := getLocalCommitData(&junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 fresh commits, got %d: %+v", len(commits), commits)
	}
	subjects := map[string]bool{}
	for _, c := range commits {
		subjects[c.Subject] = true
	}
	if !subjects["MINOR: one: first fresh commit"] || !subjects["MINOR: two: second fresh commit"] {
		t.Fatalf("unexpected subjects: %+v", commits)
	}
	if subjects["MINOR: base: another published commit"] {
		t.Fatal("published commit should not be checked")
	}
}

func TestGetLocalCommitDataUpstreamRemotePreferred(t *testing.T) {
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

	base := commitFile(t, wt, dir, "a.txt", "a", "MINOR: base: some base commit message", "alice")
	err = repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/upstream/main", base))
	if err != nil {
		t.Fatal(err)
	}

	// pushed to fork (origin) but not yet in upstream: must still be checked
	pushed := commitFile(t, wt, dir, "b.txt", "b", "MINOR: one: pushed to fork only", "alice")
	err = repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/origin/feature", pushed))
	if err != nil {
		t.Fatal(err)
	}

	commitFile(t, wt, dir, "c.txt", "c", "MINOR: two: local only commit", "alice")

	commits, _, err := getLocalCommitData(&junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits ahead of upstream, got %d: %+v", len(commits), commits)
	}
	subjects := map[string]bool{}
	for _, c := range commits {
		subjects[c.Subject] = true
	}
	if !subjects["MINOR: one: pushed to fork only"] || !subjects["MINOR: two: local only commit"] {
		t.Fatalf("unexpected subjects: %+v", commits)
	}
}

func TestGetLocalCommitDataNothingAheadOfRemote(t *testing.T) {
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
	err = repo.Storer.SetReference(plumbing.NewHashReference("refs/remotes/origin/main", head))
	if err != nil {
		t.Fatal(err)
	}

	commits, diffs, err := getLocalCommitData(&junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 0 || len(diffs) != 0 {
		t.Fatalf("expected nothing to check, got commits=%+v diffs=%+v", commits, diffs)
	}
}

func TestGetLocalCommitDataNoRemotesFallsBackToAuthor(t *testing.T) {
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

	commitFile(t, wt, dir, "a.txt", "a", "MINOR: base: some base commit message", "alice")
	commitFile(t, wt, dir, "b.txt", "b", "MINOR: one: first fresh commit", "bob")
	commitFile(t, wt, dir, "c.txt", "c", "MINOR: two: second fresh commit", "bob")

	commits, _, err := getLocalCommitData(&junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}

	// no remote refs: author-change heuristic collects bob's commits only
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits from author heuristic, got %d: %+v", len(commits), commits)
	}
}
