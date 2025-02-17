package main

import (
	"fmt"
	"log"
	"os"
	"path"

	"github.com/haproxytech/check-commit/v5/aspell"
	"github.com/haproxytech/check-commit/v5/version"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load(".env")
	err := version.Set()
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) == 2 {
		for _, arg := range os.Args[1:] {
			if arg == "version" {
				fmt.Println("check-commit", version.Version)
				fmt.Println("built from:", version.Repo)
				fmt.Println("commit date:", version.CommitDate)
				os.Exit(0)
			}
			if arg == "tag" {
				fmt.Println(version.Tag)
				os.Exit(0)
			}
		}
	}
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	var repoPath string

	if len(os.Args) < requiredCmdlineArgs {
		repoPath = "."
	} else {
		repoPath = os.Args[1]
	}

	aspellCheck, err := aspell.New(path.Join(repoPath, ".aspell.yml"))
	if err != nil {
		log.Fatalf("error reading aspell exceptions: %s", err)
	}

	commitPolicy, err := LoadCommitPolicy(path.Join(repoPath, ".check-commit.yml"))
	if err != nil {
		log.Fatalf("error reading configuration: %s", err)
	}

	if commitPolicy.IsEmpty() {
		log.Printf("WARNING: using empty configuration (i.e. no verification)")
	}

	gitEnv, err := readGitEnvironment()
	if err != nil {
		log.Fatalf("couldn't auto-detect running environment, please set GITHUB_REF and GITHUB_BASE_REF manually: %s", err)
	}

	subjects, messages, content, err := getCommitData(gitEnv)
	if err != nil {
		log.Fatalf("error getting commit data: %s", err)
	}

	if err := commitPolicy.CheckSubjectList(subjects); err != nil {
		log.Printf("encountered one or more commit message errors\n")
		log.Fatalf("%s\n", commitPolicy.HelpText)
	}

	err = aspellCheck.Check(subjects, messages, content)
	if err != nil {
		log.Printf("encountered one or more commit message spelling errors\n")
		// log.Fatalf("%s\n", err)
		log.Fatalf("%s\n", aspellCheck.HelpText)
	}

	log.Printf("check completed without errors\n")
}
