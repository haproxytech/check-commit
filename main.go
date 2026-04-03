package main

import (
	"fmt"
	"log"
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
	err := version.Set()
	if err != nil {
		log.Fatal(err)
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
				fmt.Fprintf(os.Stderr, "error: %s\n", err)
				os.Exit(1)
			}
			fmt.Println(".aspell.yml created")
			os.Exit(0)
		}
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)

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
		log.Printf("JUNIT_FILE is set to %s\n", junitFile)
		err := ts.Write(junitFile)
		if err != nil {
			log.Fatalf("failed to save junit report: %s", err)
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
		log.Printf("error reading aspell configuration: %s", err)
		exitCode = 1
		return
	}

	commitPolicy, err := LoadCommitPolicy(path.Join(repoPath, ".check-commit.yml"))
	if err != nil {
		junitSuite.AddMessageFailed(".check-commit.yml", "error reading configuration", err.Error())
		log.Printf("error reading configuration: %s", err)
		exitCode = 1
		return
	}

	if commitPolicy.IsEmpty() {
		junitSuite.AddMessageOK("", "using empty configuration", "")
		log.Print("WARNING: using empty configuration (i.e. no verification)")
	}

	gitEnv, err := readGitEnvironment()
	if err != nil {
		junitSuite.AddMessageFailed("", "couldn't auto-detect running environment, please set GITHUB_REF and GITHUB_BASE_REF manually", err.Error())
		log.Printf("couldn't auto-detect running environment, please set GITHUB_REF and GITHUB_BASE_REF manually: %s", err)
		exitCode = 1
		return
	}

	subjects, messages, content, err := getCommitData(gitEnv, junitSuite)
	if err != nil {
		log.Printf("error getting commit data: %s", err)
		exitCode = 1
		return
	}

	if err := commitPolicy.CheckSubjectList(subjects, junitSuite); err != nil {
		junitSuite.AddMessageFailed("commit subject check", "commit subject policy violation", commitPolicy.HelpText)
		log.Printf("%s\n", commitPolicy.HelpText)
		exitCode = 1
		return
	}

	gitHashes := getGitHashes(repoPath)

	err = aspellCheck.Check(subjects, messages, content, junitSuite, gitHashes)
	if err != nil {
		log.Print("encountered one or more commit message spelling errors")
		// log.Fatalf("%s\n", err)
		log.Printf("%s\n", aspellCheck.HelpText)
		exitCode = 1
		return
	}

	log.Print("check completed without errors")
}
