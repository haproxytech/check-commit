package match

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

var (
	// C identifiers
	cFuncRe   = regexp.MustCompile(`\b(\w+)\s*\([^)]*\)\s*\{`)
	cVarRe    = regexp.MustCompile(`\b(?:int|char|long|short|unsigned|signed|float|double|void|size_t|ssize_t|uint\d*_t|int\d*_t|bool|const)\s+\*?\s*(\w+)`)
	cStructRe = regexp.MustCompile(`\b(?:struct|enum|union|typedef)\s+(\w+)`)
	cDefineRe = regexp.MustCompile(`#define\s+(\w+)`)
	cMacroRe  = regexp.MustCompile(`\b([A-Z][A-Z0-9_]{2,})\b`)

	// Python identifiers
	pyFuncRe  = regexp.MustCompile(`\bdef\s+(\w+)\s*\(`)
	pyClassRe = regexp.MustCompile(`\bclass\s+(\w+)`)
	pyVarRe   = regexp.MustCompile(`\b(\w+)\s*=\s*`)

	// Generic identifier pattern for other languages
	genericFuncRe = regexp.MustCompile(`\bfunction\s+(\w+)`)
	genericVarRe  = regexp.MustCompile(`\b(?:let|const|var)\s+(\w+)`)

	// Matches any identifier-like token: camelCase, PascalCase, snake_case, UPPER_CASE
	// Must start with a letter and be at least 2 chars.
	identifierTokenRe = regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9_]{1,})\b`)
)

// GetIdentifiersFromContent extracts function names, variable names, type names,
// and other identifiers from code content. It auto-detects the language based on
// the file extension. For Go files, it uses the go/ast parser for accurate extraction.
func GetIdentifiersFromContent(filename, content string) []string {
	if strings.HasSuffix(filename, ".go") {
		return getGoIdentifiers(content)
	}
	return getRegexIdentifiers(filename, content)
}

// getGoIdentifiers uses go/ast to extract all identifiers from Go source.
// Since the input is often a diff (only added lines), it tries multiple
// strategies to parse the content, then falls back to generic token extraction.
func getGoIdentifiers(content string) []string {
	// Try parsing as-is first (complete file)
	if ids := parseGoSource(content); len(ids) > 0 {
		return ids
	}

	// Diff content often has "+" prefixes from git — strip them
	stripped := stripDiffPrefixes(content)
	if ids := parseGoSource(stripped); len(ids) > 0 {
		return ids
	}

	// Wrap in a synthetic file to parse partial code (e.g. function bodies)
	wrapped := "package _x\nfunc _() {\n" + stripped + "\n}"
	if ids := parseGoSource(wrapped); len(ids) > 0 {
		return ids
	}

	// Wrap as top-level declarations (e.g. type/var/const blocks)
	wrapped = "package _x\n" + stripped
	if ids := parseGoSource(wrapped); len(ids) > 0 {
		return ids
	}

	// AST failed — extract all identifier-like tokens
	return extractAllTokens(stripped)
}

func stripDiffPrefixes(content string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(content, "\n") {
		if trimmed, ok := strings.CutPrefix(line, "+"); ok {
			b.WriteString(trimmed)
		} else {
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// parseGoSource parses Go source and collects every ast.Ident node.
// It accepts partial ASTs (parser may return a usable tree even with errors).
func parseGoSource(src string) []string {
	fset := token.NewFileSet()
	// SkipObjectResolution is faster and we don't need resolved objects.
	// Even with parse errors, the parser may return a partial AST — use it.
	f, _ := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if f == nil {
		return nil
	}

	seen := map[string]struct{}{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			addIdent(seen, node.Name)
		case *ast.Field:
			// Extract identifiers from struct tags
			if node.Tag != nil {
				extractTagIdentifiers(seen, node.Tag.Value)
			}
		case *ast.BasicLit:
			// Extract identifier-like tokens from string literals
			// (catches words inside regex patterns, format strings, etc.)
			if node.Kind == token.STRING {
				extractStringLiteralTokens(seen, node.Value)
			}
		}
		return true
	})

	if len(seen) == 0 {
		return nil
	}

	result := make([]string, 0, len(seen))
	for word := range seen {
		result = append(result, word)
	}
	return result
}

func addIdent(seen map[string]struct{}, name string) {
	if name == "" || name == "_" || len(name) < 2 {
		return
	}
	if isCommonKeyword(name) {
		return
	}
	lower := strings.ToLower(name)
	seen[lower] = struct{}{}
	// Also add underscore/hyphen-split parts and camelCase parts,
	// since aspell's checkSingle splits words the same way.
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == '_' || r == '-'
	}) {
		if len(part) >= 2 && !isCommonKeyword(part) {
			seen[part] = struct{}{}
		}
	}
}

// regexEscapeRe matches common regex escape sequences (\b, \s, \w, \d, \n, \t, etc.)
var regexEscapeRe = regexp.MustCompile(`\\[bBsSwWdDntrfvpP]`)

// extractStringLiteralTokens extracts identifier-like tokens from Go string
// literals. Regex escape sequences are stripped first so that e.g. `\bclass`
// yields `class` instead of `bclass`.
func extractStringLiteralTokens(seen map[string]struct{}, lit string) {
	// Remove Go string delimiters
	lit = strings.Trim(lit, "\"`")
	// Extract from raw content (aspell sees e.g. "bclass" from "\bclass")
	for _, m := range identifierTokenRe.FindAllString(lit, -1) {
		addIdent(seen, m)
	}
	// Also extract after stripping regex escapes (gets the real words like "class")
	cleaned := regexEscapeRe.ReplaceAllString(lit, " ")
	for _, m := range identifierTokenRe.FindAllString(cleaned, -1) {
		addIdent(seen, m)
	}
}

func extractTagIdentifiers(seen map[string]struct{}, tag string) {
	tag = strings.Trim(tag, "`")
	tagRe := regexp.MustCompile(`\w+:"([^"]*)"`)
	for _, m := range tagRe.FindAllStringSubmatch(tag, -1) {
		if len(m) > 1 {
			parts := strings.SplitN(m[1], ",", 2)
			if parts[0] != "" && parts[0] != "-" {
				addIdent(seen, parts[0])
			}
		}
	}
}

// extractAllTokens pulls every identifier-like token from the text.
// Used as a last resort when AST parsing fails completely.
func extractAllTokens(content string) []string {
	seen := map[string]struct{}{}
	for _, m := range identifierTokenRe.FindAllString(content, -1) {
		addIdent(seen, m)
	}
	result := make([]string, 0, len(seen))
	for word := range seen {
		result = append(result, word)
	}
	return result
}

func getRegexIdentifiers(filename, content string) []string {
	seen := map[string]struct{}{}

	var patterns []*regexp.Regexp
	switch {
	case strings.HasSuffix(filename, ".c") || strings.HasSuffix(filename, ".h"):
		patterns = []*regexp.Regexp{cFuncRe, cVarRe, cStructRe, cDefineRe, cMacroRe}
	case strings.HasSuffix(filename, ".py"):
		patterns = []*regexp.Regexp{pyFuncRe, pyClassRe, pyVarRe}
	case strings.HasSuffix(filename, ".js") || strings.HasSuffix(filename, ".ts") ||
		strings.HasSuffix(filename, ".jsx") || strings.HasSuffix(filename, ".tsx"):
		patterns = []*regexp.Regexp{genericFuncRe, genericVarRe, pyClassRe}
	default:
		// For unknown languages, extract all identifier-like tokens
		return extractAllTokens(content)
	}

	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			for i := 1; i < len(m); i++ {
				addIdent(seen, m[i])
			}
		}
	}

	result := make([]string, 0, len(seen))
	for word := range seen {
		result = append(result, word)
	}
	return result
}

var commonKeywords = map[string]struct{}{
	"if": {}, "else": {}, "for": {}, "while": {}, "do": {},
	"switch": {}, "case": {}, "break": {}, "continue": {},
	"return": {}, "true": {}, "false": {}, "nil": {}, "null": {},
	"void": {}, "int": {}, "char": {}, "bool": {}, "string": {},
	"func": {}, "var": {}, "const": {}, "type": {}, "struct": {},
	"interface": {}, "map": {}, "range": {}, "import": {},
	"package": {}, "defer": {}, "go": {}, "select": {},
	"chan": {}, "default": {}, "class": {}, "def": {},
	"self": {}, "this": {}, "new": {}, "delete": {},
	"try": {}, "catch": {}, "throw": {}, "finally": {},
	"public": {}, "private": {}, "protected": {}, "static": {},
	"let": {}, "of": {}, "in": {}, "is": {},
	"error": {}, "byte": {}, "rune": {},
}

func isCommonKeyword(word string) bool {
	_, ok := commonKeywords[strings.ToLower(word)]
	return ok
}
