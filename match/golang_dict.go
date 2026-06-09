package match

// goDictionary lists Go-specific words that aspell would otherwise flag in
// commit message bodies and diffs. Prepended to the imports extracted per file.
// Lowercased at use; case here is only for readability.
var goDictionary = []string{
	// keywords
	"break", "default", "func", "interface", "select",
	"case", "defer", "go", "map", "struct",
	"chan", "else", "goto", "package", "switch",
	"const", "fallthrough", "if", "range", "type",
	"continue", "for", "import", "return", "var",

	// predeclared types and builtins
	"bool", "byte", "complex64", "complex128",
	"error", "float32", "float64", "int",
	"int8", "int16", "int32", "int64", "rune", "string",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"len", "cap",

	// common stdlib identifiers
	"str", "filepath", "url", "Fatalf", "ctx",
	"Println", "Stdin", "stdout", "stderr", "Stdout", "Stderr",
	"errorf", "println", "Sprintf", "Printf", "Unmarshal",
	"Getenv", "Errorf", "Atoi", "EOF", "exec", "iter",

	// go tooling and environment variables
	"gomemlimit", "gomaxprocs", "gogc", "godebug", "goflags",
	"goos", "goarch", "gopath", "goroot", "goproxy",
	"gocache", "gomodcache", "goprivate", "gotoolchain", "cgo",

	// project-specific identifiers
	"tt", "yml", "ok", "cmd", "utf", "oauth", "args",
}
