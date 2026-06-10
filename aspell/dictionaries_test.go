package aspell

import "testing"

func TestEmbeddedLanguageDictionariesLoaded(t *testing.T) {
	// One representative word per added wordlist.
	for _, word := range []string{"gomemlimit", "gotoolchain", "kwargs", "asyncio", "pinia", "composable", "testcontainers", "leaderelection", "mgmt", "srvtcpka"} {
		if _, ok := acceptableWordsGlobal[word]; !ok {
			t.Errorf("expected %q loaded from embedded dictionaries", word)
		}
	}
}
