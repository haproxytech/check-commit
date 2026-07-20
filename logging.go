package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"golang.org/x/term"
)

// initLogging installs the global logger: tint for humans by default,
// JSON when LOG_FORMAT=json. Must run after godotenv so .env applies.
func initLogging() {
	slog.SetDefault(slog.New(newLogHandler(os.Stderr)))
}

func newLogHandler(w io.Writer) slog.Handler {
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		return slog.NewJSONHandler(w, nil)
	}
	return tint.NewHandler(w, &tint.Options{
		TimeFormat: time.TimeOnly, // 24h clock
		NoColor:    noColor(),
	})
}

// noColor: NO_COLOR always wins, LOG_COLOR forces on or off,
// GitLab/GitHub CI logs render ANSI, else require a TTY.
func noColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	switch strings.ToLower(os.Getenv("LOG_COLOR")) {
	case "1", "true", "always":
		return false
	case "0", "false", "never":
		return true
	}
	if os.Getenv("GITLAB_CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return false
	}
	return !term.IsTerminal(int(os.Stderr.Fd()))
}
