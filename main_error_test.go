package main

import (
	"errors"
	"fmt"
	"testing"
)

type junitRecorder struct {
	ok     []string
	failed []string
}

func (r *junitRecorder) AddMessageOK(name, message, details string) {
	r.ok = append(r.ok, fmt.Sprintf("%s|%s|%s", name, message, details))
}

func (r *junitRecorder) AddMessageFailed(name, message, details string) {
	r.failed = append(r.failed, fmt.Sprintf("%s|%s|%s", name, message, details))
}

func TestHandleDataErrorSkipsInCI(t *testing.T) {
	rec := &junitRecorder{}
	err := fmt.Errorf("%w: fetching commits: connection refused", errCommitDataUnavailable)

	if fatal := handleDataError(err, GITHUB, rec); fatal {
		t.Fatal("unavailable data in CI must not fail the run")
	}
	if len(rec.failed) != 0 {
		t.Fatalf("no junit failure expected, got %v", rec.failed)
	}
	if len(rec.ok) != 1 {
		t.Fatalf("expected one junit OK entry, got %v", rec.ok)
	}
	// generic message, error detail in the body
	if want := "commit data|commit checks skipped: could not determine commits to check|"; rec.ok[0][:len(want)] != want {
		t.Errorf("unexpected junit entry: %q", rec.ok[0])
	}
	if !errors.Is(err, errCommitDataUnavailable) || rec.ok[0][len(rec.ok[0])-len("connection refused"):] != "connection refused" {
		t.Errorf("junit body must carry the error detail: %q", rec.ok[0])
	}
}

func TestHandleDataErrorFailsLocally(t *testing.T) {
	rec := &junitRecorder{}
	err := fmt.Errorf("%w: opening local git repository", errCommitDataUnavailable)

	if fatal := handleDataError(err, LOCAL, rec); !fatal {
		t.Fatal("local data errors must fail the run")
	}
	if len(rec.failed) != 1 {
		t.Fatalf("expected one junit failure, got %v", rec.failed)
	}
}

func TestHandleDataErrorValidationFailsInCI(t *testing.T) {
	rec := &junitRecorder{}
	err := errors.New("empty line between subject and body is required: abc12345 subject")

	if fatal := handleDataError(err, GITLAB, rec); !fatal {
		t.Fatal("validation errors must fail the run even in CI")
	}
	if len(rec.ok) != 0 {
		t.Fatalf("no junit OK entry expected, got %v", rec.ok)
	}
}
