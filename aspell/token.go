package aspell

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// glabTokenFn is a seam so tests can stub the glab lookup.
var glabTokenFn = glabToken

var (
	glabCacheMu sync.Mutex
	glabCache   = map[string]string{}
)

// glabToken returns glab's stored token for host, "" when unavailable.
// glab only returns tokens for hosts the user logged into, so tokens
// cannot leak to arbitrary configured URLs. Cached per host.
func glabToken(host string) string {
	if host == "" {
		return ""
	}
	glabCacheMu.Lock()
	defer glabCacheMu.Unlock()
	if token, ok := glabCache[host]; ok {
		return token
	}
	token := ""
	if path, err := exec.LookPath("glab"); err == nil {
		if out, cmdErr := exec.Command(path, "config", "get", "token", "--host", host).Output(); cmdErr == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	if token != "" {
		slog.Info("using GitLab token from glab", "host", host)
	}
	glabCache[host] = token
	return token
}

// gitlabToken resolves a GitLab token: env var first, then glab's store.
func gitlabToken(tokenEnv, host string) string {
	if tokenEnv != "" {
		if token := os.Getenv(tokenEnv); token != "" {
			return token
		}
	}
	return glabTokenFn(host)
}
