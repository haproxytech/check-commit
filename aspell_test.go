package main

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/haproxytech/check-commit/v5/aspell"
	"github.com/haproxytech/check-commit/v5/junit"
)

func Test_Aspell(t *testing.T) {
	aspellCheck, err := aspell.New(".aspell.yml")
	if err != nil {
		t.Errorf("checkWithAspell() error = %v", err)
	}

	filename := "README.md"
	// filename := "check.go"
	readmeFile, err := os.Open(filename)
	if err != nil {
		t.Errorf("could not open "+filename+" file: %v", err)
	}
	defer readmeFile.Close()

	scanner := bufio.NewScanner(readmeFile)
	var readme strings.Builder
	for scanner.Scan() {
		readme.WriteString(scanner.Text() + "\n")
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("could not read "+filename+" file: %v", err)
	}
	err = aspellCheck.Check([]aspell.Commit{{Subject: "subject", Message: "body"}}, []aspell.CommitDiff{
		{Files: map[string]string{filename: readme.String()}},
	}, &junit.JunitSuiteDummy{}, nil)
	if err != nil {
		t.Errorf("checkWithAspell() error = %v", err)
	}
}
