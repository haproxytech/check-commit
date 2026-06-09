package match

import (
	"os"
	"strings"
)

func findImports(fileContent, startStr, endStr string, process func(data string)) {
	start := strings.Index(fileContent, startStr)
	if start == -1 {
		return
	}
	start += len(startStr)
	content := fileContent[start:]
	before, _, ok := strings.Cut(content, endStr)
	if !ok {
		return
	}
	raw := before
	process(raw)
}

func GetImportWordsFromGoFile(filename string) []string {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil
	}
	fileContent := string(data)

	importWords := append([]string(nil), goDictionary...)
	findImports(fileContent, "import (", ")", func(data string) {
		words := strings.FieldsFunc(data, func(r rune) bool {
			return r == '\n' || r == '\t' || r == '/' || r == '.'
		})
		for _, word := range words {
			word = strings.Trim(word, ` "`)
			if word != "" {
				importWords = append(importWords, word)
			}
		}
	})

	for i := range len(importWords) {
		importWords[i] = strings.ToLower(importWords[i])
	}

	return importWords
}
