package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"testing"
)

func TestNewLogHandlerJSON(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")

	var buf bytes.Buffer
	logger := slog.New(newLogHandler(&buf))
	logger.Error("aspell check failed", "commit", "abc12345", "file", "f.txt")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, buf.String())
	}
	if entry["msg"] != "aspell check failed" || entry["commit"] != "abc12345" || entry["level"] != "ERROR" {
		t.Errorf("unexpected entry: %v", entry)
	}
}

func TestNewLogHandlerDefaultUses24hTime(t *testing.T) {
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("NO_COLOR", "1") // stable output for assertions

	var buf bytes.Buffer
	logger := slog.New(newLogHandler(&buf))
	logger.Info("checking local commits", "count", 2)

	out := buf.String()
	if !regexp.MustCompile(`^\d{2}:\d{2}:\d{2} INF checking local commits count=2`).MatchString(out) {
		t.Errorf("unexpected tint output: %q", out)
	}
	if bytes.Contains(buf.Bytes(), []byte("\x1b[")) {
		t.Errorf("ANSI codes present despite NO_COLOR: %q", out)
	}
}

func TestNoColorPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		noColor  string
		logColor string
		want     bool
	}{
		{"NO_COLOR wins over LOG_COLOR", "1", "always", true},
		{"LOG_COLOR forces color", "", "1", false},
		{"LOG_COLOR true forces color", "", "true", false},
		{"default without TTY", "", "", true}, // test stderr is not a terminal
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("LOG_COLOR", tt.logColor)
			if got := noColor(); got != tt.want {
				t.Errorf("noColor() = %v, want %v", got, tt.want)
			}
		})
	}
}
