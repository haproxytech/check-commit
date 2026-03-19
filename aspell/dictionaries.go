package aspell

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed dictionaries/*.txt
var embeddedDictionaries embed.FS

func init() {
	entries, err := fs.ReadDir(embeddedDictionaries, "dictionaries")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := embeddedDictionaries.ReadFile("dictionaries/" + entry.Name())
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			word := strings.TrimSpace(line)
			if word == "" || strings.HasPrefix(word, "#") {
				continue
			}
			acceptableWordsGlobal[strings.ToLower(word)] = struct{}{}
		}
	}
}
