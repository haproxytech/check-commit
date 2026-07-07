package aspell

import (
	"os/exec"
	"slices"
	"testing"
)

// Integration: this repo's own go.mod must yield its dependency words.
func TestCollectManifestWordsOwnRepo(t *testing.T) {
	words := collectManifestWords("..")
	for _, want := range []string{"fatih", "camelcase", "haproxytech"} {
		if !slices.Contains(words, want) {
			t.Errorf("collectManifestWords(..) missing %q", want)
		}
	}
}

// Integration: this repo's paths must yield path-segment words.
func TestCollectPathWordsOwnRepo(t *testing.T) {
	words := collectPathWords("..")
	for _, want := range []string{"dictionaries", "aspell", "junit"} {
		if !slices.Contains(words, want) {
			t.Errorf("collectPathWords(..) missing %q", want)
		}
	}
}

func TestCollectManifestWordsMissingDir(t *testing.T) {
	if words := collectManifestWords(t.TempDir()); len(words) != 0 {
		t.Errorf("collectManifestWords on empty dir = %v, want empty", words)
	}
}

// checkSingle must accept words present in RepoWords.
func TestCheckSingleConsultsRepoWords(t *testing.T) {
	if _, err := exec.LookPath("aspell"); err != nil {
		t.Skip("aspell binary not available")
	}
	a := Aspell{MinLength: 3, RepoWords: map[string]struct{}{"zzqxj": {}}}
	if err := a.checkSingle("zzqxj", nil); err != nil {
		t.Errorf("checkSingle with RepoWords = %v, want nil", err)
	}
	b := Aspell{MinLength: 3}
	if err := b.checkSingle("zzqxj", nil); err == nil {
		t.Error("checkSingle without RepoWords = nil, want error")
	}
}
