package aspell

import "testing"

func TestEmbeddedLanguageDictionariesLoaded(t *testing.T) {
	// One representative word per added language wordlist.
	for _, word := range []string{"gomemlimit", "gotoolchain", "kwargs", "asyncio", "pinia", "composable"} {
		if _, ok := acceptableWordsGlobal[word]; !ok {
			t.Errorf("expected %q loaded from embedded dictionaries", word)
		}
	}
}
