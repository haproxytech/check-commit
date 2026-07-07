package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v56/github"
	"github.com/haproxytech/check-commit/v5/junit"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestGithubAPICommitDataSingleFilesFetch(t *testing.T) {
	filesCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/own/repo/pulls/7/commits", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `[
			{"sha":"1111111111111111","commit":{"sha":"1111111111111111","message":"first subject"}},
			{"sha":"2222222222222222","commit":{"sha":"2222222222222222","message":"second subject"}}
		]`)
	})
	mux.HandleFunc("/repos/own/repo/pulls/7/files", func(w http.ResponseWriter, _ *http.Request) {
		filesCalls++
		_, _ = fmt.Fprint(w, `[{"filename":"a.txt","patch":"+added line"}]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := github.NewClient(nil)
	base, _ := url.Parse(srv.URL + "/")
	client.BaseURL = base

	commits, diffs, err := githubAPICommitData(context.Background(), client,
		"own/repo", "refs/pull/7/merge", "pull_request", &junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	if filesCalls != 1 {
		t.Errorf("files endpoint called %d times, want 1", filesCalls)
	}
	if len(diffs) != 1 {
		t.Fatalf("diffs entries = %d, want 1", len(diffs))
	}
	if diffs[0].Hash != "" {
		t.Errorf("API diffs are not attributable, hash = %q", diffs[0].Hash)
	}
	if diffs[0].Files["a.txt"] != "+added line\n" {
		t.Errorf("diff content = %v", diffs[0].Files)
	}
}

func TestGithubAPICommitDataFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := github.NewClient(nil)
	base, _ := url.Parse(srv.URL + "/")
	client.BaseURL = base

	_, _, err := githubAPICommitData(context.Background(), client,
		"own/repo", "refs/pull/7/merge", "pull_request", &junit.JunitSuiteDummy{})
	if !errors.Is(err, errCommitDataUnavailable) {
		t.Fatalf("expected errCommitDataUnavailable, got %v", err)
	}
}

func TestGitlabAPICommitDataSingleDiffsFetch(t *testing.T) {
	diffsCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v4/projects/42/merge_requests/7/commits", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `[
			{"id":"aaaaaaaaaaaaaaaa","short_id":"aaaaaaaa","message":"first subject"},
			{"id":"bbbbbbbbbbbbbbbb","short_id":"bbbbbbbb","message":"second subject"}
		]`)
	})
	mux.HandleFunc("/api/v4/projects/42/merge_requests/7/diffs", func(w http.ResponseWriter, _ *http.Request) {
		diffsCalls++
		_, _ = fmt.Fprint(w, `[{"new_path":"a.txt","diff":"+added line"}]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := gitlab.NewClient("", gitlab.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	commits, diffs, err := gitlabAPICommitData(client, "7", "42", &junit.JunitSuiteDummy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	if diffsCalls != 1 {
		t.Errorf("diffs endpoint called %d times, want 1", diffsCalls)
	}
	if len(diffs) != 1 {
		t.Fatalf("diffs entries = %d, want 1", len(diffs))
	}
	if diffs[0].Files["a.txt"] != "+added line\n" {
		t.Errorf("diff content = %v", diffs[0].Files)
	}
}
