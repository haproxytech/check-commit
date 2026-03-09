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

	var importWords []string
	importWords = append(importWords,
		"break", "default", "func", "interface", "select",
		"case", "defer", "go", "map", "struct",
		"chan", "else", "goto", "package", "switch",
		"const", "fallthrough", "if", "range", "type",
		"continue", "for", "import", "return", "var",
		"bool", "byte", "complex64", "complex128",
		"error", "float32", "float64", "int",
		"int8", "int16", "int32", "int64", "rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"str", "len", "cap", "filepath", "url", "Fatalf", "ctx",
		"Println", "Stdin", "stdout", "stderr", "Stdout", "Stderr",
		"errorf", "println", "Sprintf", "Printf", "Unmarshal", "args",
		"Getenv", "Errorf", "tt", "yml", "ok", "cmd", "utf", "Atoi",
		"oauth", "EOF", "exec", "iter",
	)
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
