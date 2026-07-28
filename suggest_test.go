package main

import (
	"strings"
	"testing"

	"github.com/haproxytech/check-commit/v5/junit"
)

func TestCheckSubjectSuggestions(t *testing.T) {
	t.Parallel()

	c, _ := LoadCommitPolicy("")

	suggestionTests := []struct {
		name        string
		subject     string
		wantContain string // expected substring of the error, "" means no suggestion
	}{
		{
			name:        "typo in tag",
			subject:     "DOCS/MINOR: update build targets and process isolation details in README",
			wantContain: "did you mean 'DOC/MINOR'?",
		},
		{
			name:        "typo in severity",
			subject:     "DOC/MINR: update build targets and process isolation details in README",
			wantContain: "did you mean 'DOC/MINOR'?",
		},
		{
			name:        "lowercase tag",
			subject:     "doc: update build targets and process isolation details in README",
			wantContain: "did you mean 'DOC'?",
		},
		{
			name:        "typo in severity-only tag",
			subject:     "MINR: update build targets and process isolation details in README",
			wantContain: "did you mean 'MINOR'?",
		},
		{
			name:        "unrelated tag gets no suggestion",
			subject:     "WRONG: update build targets and process isolation details in README",
			wantContain: "",
		},
	}

	for _, tt := range suggestionTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := c.CheckSubject([]byte(tt.subject), &junit.JunitSuiteDummy{})
			if err == nil {
				t.Fatalf("CheckSubject() expected error for %q, got nil", tt.subject)
			}
			if tt.wantContain == "" {
				if strings.Contains(err.Error(), "did you mean") {
					t.Errorf("CheckSubject() error = %v, want no suggestion", err)
				}
				return
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("CheckSubject() error = %v, want substring %q", err, tt.wantContain)
			}
		})
	}
}
