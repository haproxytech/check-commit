package aspell

import (
	"strings"
	"testing"

	"github.com/haproxytech/check-commit/v5/junit"
)

// stubAspell flags every word not present in known, without spawning aspell.
func stubAspell(t *testing.T, known map[string]bool) {
	t.Helper()
	orig := aspellExecFn
	aspellExecFn = func(data string, _ ...string) (string, error) {
		var unknown []string
		for tok := range strings.FieldsSeq(data) {
			if !known[strings.ToLower(tok)] {
				unknown = append(unknown, tok)
			}
		}
		return strings.Join(unknown, "\n"), nil
	}
	t.Cleanup(func() { aspellExecFn = orig })
}

func Test_checkFiles_attributesErrorsToCommit(t *testing.T) {
	stubAspell(t, map[string]bool{"known": true, "words": true})

	a := Aspell{Mode: modeAll, MinLength: 3, NoIgnoreIdentifiers: true}
	content := []CommitDiff{
		{Hash: "abc12345", Subject: "MINOR: demo: add the f file", Files: map[string]string{"f.txt": "known words qqzvxbad"}},
		{Files: map[string]string{"g.txt": "known words wwqkbad"}},
	}

	var response strings.Builder
	a.checkFiles(content, nil, &junit.JunitSuiteDummy{}, &response)

	out := response.String()
	if !strings.Contains(out, `commit abc12345 "MINOR: demo: add the f file" f.txt`) {
		t.Errorf("attributed finding missing commit hash and subject: %q", out)
	}
	if !strings.Contains(out, "g.txt: aspell") {
		t.Errorf("unattributed finding should report file only: %q", out)
	}
	if strings.Contains(out, "commit  g.txt") {
		t.Errorf("unattributed finding must not have empty commit prefix: %q", out)
	}
}
