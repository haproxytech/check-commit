package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/haproxytech/check-commit/v5/aspell"
	"github.com/haproxytech/check-commit/v5/junit"
	"github.com/haproxytech/check-commit/v5/version"
	"github.com/joho/godotenv"
	junit_report "github.com/oktalz/junit-report"
)

var exitCode = 0

func main() {
	_ = godotenv.Load(".env")
	initLogging()
	err := version.Set()
	if err != nil {
		slog.Error("failed to read build info", "err", err)
		os.Exit(1)
	}
	if len(os.Args) >= 2 && os.Args[1] == "append" {
		if len(os.Args) < 3 {
			_, _ = fmt.Fprintln(os.Stderr, "usage: check-commit append <word> [word...]")
			os.Exit(1)
		}
		added, skipped, err := aspell.Append(".aspell.yml", os.Args[2:])
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
			os.Exit(1)
		}
		for _, w := range added {
			fmt.Println("added:", w)
		}
		for _, w := range skipped {
			fmt.Println("already allowed:", w)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "version":
			fmt.Println("check-commit", version.Version)
			fmt.Println("built from:", version.Repo)
			fmt.Println("commit date:", version.CommitDate)
			os.Exit(0)
		case "tag":
			fmt.Println(version.Tag)
			os.Exit(0)
		case "help":
			aspell.PrintHelp()
			os.Exit(0)
		case "init":
			if err := aspell.Init(".aspell.yml"); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(".aspell.yml created")
			os.Exit(0)
		}
	}
	slog.Info("check-commit", "version", version.Version)

	// JUNIT_FILE
	ts := junit_report.NewTestSuites()
	junitFile := os.Getenv("JUNIT_FILE")
	var junitSuite junit.Interface
	if junitFile != "" {
		junitSuite = ts.GetOrCreateSuite("check-commit")
	} else {
		junitSuite = &junit.JunitSuiteDummy{}
	}
	start(junitSuite)
	if junitFile != "" {
		if exitCode == 0 {
			junitSuite.AddMessageOK("check-commit", "check-commit completed successfully", "")
		}
		slog.Info("writing junit report", "file", junitFile)
		err := ts.Write(junitFile)
		if err != nil {
			slog.Error("failed to save junit report", "err", err)
			os.Exit(1)
		}
	}
	os.Exit(exitCode)
}

func start(junitSuite junit.Interface) {
	var repoPath string
	if len(os.Args) < requiredCmdlineArgs {
		repoPath = "."
	} else {
		repoPath = os.Args[1]
	}

	aspellConfigFile := path.Join(repoPath, ".aspell.yml")
	aspellCheck, err := aspell.New(aspellConfigFile)
	if err != nil {
		junitSuite.AddMessageFailed(".aspell.yml", "error reading aspell configuration", err.Error())
		slog.Error("error reading aspell configuration", "err", err)
		exitCode = 1
		return
	}

	commitPolicy, err := LoadCommitPolicy(path.Join(repoPath, ".check-commit.yml"))
	if err != nil {
		junitSuite.AddMessageFailed(".check-commit.yml", "error reading configuration", err.Error())
		slog.Error("error reading configuration", "err", err)
		exitCode = 1
		return
	}

	if commitPolicy.IsEmpty() {
		junitSuite.AddMessageOK("", "using empty configuration", "")
		slog.Warn("using empty configuration (i.e. no verification)")
	}

	gitEnv, err := readGitEnvironment()
	if err != nil {
		junitSuite.AddMessageFailed("", "couldn't auto-detect running environment, please set GITHUB_REF and GITHUB_BASE_REF manually", err.Error())
		slog.Error("couldn't auto-detect running environment, please set GITHUB_REF and GITHUB_BASE_REF manually", "err", err)
		exitCode = 1
		return
	}

	commits, content, err := getCommitData(gitEnv, junitSuite)
	if err != nil {
		if handleDataError(err, gitEnv, junitSuite) {
			exitCode = 1
		}
		return
	}

	if err := commitPolicy.CheckSubjectList(commits, junitSuite); err != nil {
		junitSuite.AddMessageFailed("commit subject check", "commit subject policy violation", commitPolicy.HelpText)
		_, _ = fmt.Fprintln(os.Stderr, commitPolicy.HelpText)
		exitCode = 1
		return
	}

	gitHashes := getGitHashes(repoPath)

	err = aspellCheck.Check(commits, content, junitSuite, gitHashes)
	if err != nil {
		slog.Error("encountered one or more commit message spelling errors")
		_, _ = fmt.Fprintln(os.Stderr, aspellCheck.HelpText)
		exitCode = 1
		return
	}

	slog.Info("check completed without errors")
}

// handleDataError records a commit data failure; returns true when the run
// must fail. In CI, unavailable data skips checks with details in the junit body.
func handleDataError(err error, gitEnv string, junitSuite junit.Interface) bool {
	if gitEnv != LOCAL && errors.Is(err, errCommitDataUnavailable) {
		slog.Warn("commit checks skipped", "err", err)
		junitSuite.AddMessageOK("commit data", "commit checks skipped: could not determine commits to check", err.Error())
		return false
	}
	slog.Error("error getting commit data", "err", err)
	if errors.Is(err, errCommitDataUnavailable) {
		junitSuite.AddMessageFailed("commit data", "error getting commit data", err.Error())
	}
	return true
}
