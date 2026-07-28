package match

import (
	"slices"
	"strings"
	"testing"
)

func TestGetImportWordsFromFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     []string
	}{
		{"test 1", "golang_test.go", []string{
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
			"gomemlimit", "gomaxprocs", "gogc", "godebug", "goflags",
			"goos", "goarch", "gopath", "goroot", "goproxy",
			"gocache", "gomodcache", "goprivate", "gotoolchain", "cgo",
			"strings", "slices", "testing",
		}},
		{"test 2", "match.go", []string{
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
			"gomemlimit", "gomaxprocs", "gogc", "godebug", "goflags",
			"goos", "goarch", "gopath", "goroot", "goproxy",
			"gocache", "gomodcache", "goprivate", "gotoolchain", "cgo",
			"filepath", "path", "strings", "regexp",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetImportWordsFromGoFile(tt.filename)
			slices.Sort(got)
			slices.Sort(tt.want)
			if !slices.Equal(got, tt.want) {
				for i, g := range slices.Backward(got) {
					for j, v := range slices.Backward(tt.want) {
						if strings.EqualFold(g, v) {
							got = remove(got, i)
							tt.want = remove(tt.want, j)
							break
						}
					}
				}
				if len(got) > 0 || len(tt.want) > 0 {
					t.Errorf("extra result = %v, extra wanted %v", got, tt.want)
				}
			}
		})
	}
}

func remove(slice []string, i int) []string {
	return append(slice[:i], slice[i+1:]...)
}
