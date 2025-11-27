package main

import (
	"bufio"
	"log"
	"os"
	"testing"

	"github.com/haproxytech/check-commit/v5/aspell"
	"github.com/haproxytech/check-commit/v5/junit"
)

func Test_Aspell(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	aspell, err := aspell.New(".aspell.yml")
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
	readme := ""
	for scanner.Scan() {
		readme += scanner.Text() + "\n"
	}
	if err := scanner.Err(); err != nil {
		t.Errorf("could not read "+filename+" file: %v", err)
	}
	err = aspell.Check([]string{"subject"}, []string{"body"}, []map[string]string{
		{filename: readme},
	}, &junit.JunitSuiteDummy{})
	if err != nil {
		t.Errorf("checkWithAspell() error = %v", err)
	}
}
