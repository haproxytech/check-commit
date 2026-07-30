package aspell

import (
	"net/http"
	"testing"
)

func stubGlab(t *testing.T, fn func(host string) string) {
	t.Helper()
	orig := glabTokenFn
	glabTokenFn = fn
	t.Cleanup(func() { glabTokenFn = orig })
}

func Test_gitlabToken(t *testing.T) {
	t.Run("env token wins, glab not consulted", func(t *testing.T) {
		t.Setenv("TEST_GL_TOKEN", "envtoken")
		stubGlab(t, func(string) string {
			t.Error("glab consulted despite env token")
			return ""
		})
		if got := gitlabToken("TEST_GL_TOKEN", "gitlab.example.com"); got != "envtoken" {
			t.Errorf("got %q, want envtoken", got)
		}
	})
	t.Run("empty env falls back to glab", func(t *testing.T) {
		t.Setenv("TEST_GL_TOKEN", "")
		stubGlab(t, func(host string) string {
			if host != "gitlab.example.com" {
				t.Errorf("host = %q", host)
			}
			return "glabtoken"
		})
		if got := gitlabToken("TEST_GL_TOKEN", "gitlab.example.com"); got != "glabtoken" {
			t.Errorf("got %q, want glabtoken", got)
		}
	})
	t.Run("no env configured falls back to glab", func(t *testing.T) {
		stubGlab(t, func(string) string { return "glabtoken" })
		if got := gitlabToken("", "gitlab.example.com"); got != "glabtoken" {
			t.Errorf("got %q, want glabtoken", got)
		}
	})
	t.Run("no token anywhere", func(t *testing.T) {
		stubGlab(t, func(string) string { return "" })
		if got := gitlabToken("", "gitlab.example.com"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func Test_setWikiAuth_glabFallback(t *testing.T) {
	stubGlab(t, func(host string) string {
		if host == "gitlab.example.com" {
			return "glabtoken"
		}
		return ""
	})
	req, err := http.NewRequest(http.MethodGet, "https://gitlab.example.com/api/v4/projects/p/wikis/w", nil)
	if err != nil {
		t.Fatal(err)
	}
	setWikiAuth(req, RemoteFile{})
	if got := req.Header.Get("PRIVATE-TOKEN"); got != "glabtoken" {
		t.Errorf("PRIVATE-TOKEN = %q, want glabtoken", got)
	}
}

func Test_glabToken_emptyHost(t *testing.T) {
	if got := glabToken(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
