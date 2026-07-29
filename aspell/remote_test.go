package aspell

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func serveContent(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func Test_fetchRemoteFile_formats(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "gitlab wiki CRLF plain fence with list items",
			body: `{"content": "` + "```" + `\r\n- ACL\r\n- eab\r\n- verifier\r\n` + "```" + `"}`,
			want: []string{"ACL", "eab", "verifier"},
		},
		{
			name: "yaml block LF",
			body: `{"content": "` + "```yaml" + `\n- word1\n- word2\n` + "```" + `"}`,
			want: []string{"word1", "word2"},
		},
		{
			name: "yaml block CRLF",
			body: `{"content": "` + "```yaml" + `\r\n- word1\r\n- word2\r\n` + "```" + `"}`,
			want: []string{"word1", "word2"},
		},
		{
			name: "plain newline-separated",
			body: `{"content": "word1\nword2"}`,
			want: []string{"word1", "word2"},
		},
		{
			name: "plain CRLF-separated",
			body: `{"content": "word1\r\nword2\r\n"}`,
			want: []string{"word1", "word2"},
		},
		{
			name: "JSON array",
			body: `{"content": ["word1", "word2"]}`,
			want: []string{"word1", "word2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serveContent(t, tt.body)
			a := Aspell{RemoteFile: RemoteFile{URL: srv.URL, AllowedItemsKey: "content"}}
			got, err := fetchRemoteFile(a)
			if err != nil {
				t.Fatalf("fetchRemoteFile() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("fetchRemoteFile() = %q, want %q", got, tt.want)
			}
		})
	}
}
