package main

import (
	"fmt"
	"strings"
)

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}

	return prev[len(b)]
}

// closestMatch returns the candidate nearest to word, or "" when none is
// within a small edit distance (1 for short words, 2 otherwise).
func closestMatch(word string, candidates []string) string {
	maxDist := 1
	if len(word) > 4 {
		maxDist = 2
	}

	best := ""
	bestDist := maxDist + 1
	for _, candidate := range candidates {
		if d := levenshtein(word, candidate); d < bestDist {
			best = candidate
			bestDist = d
		}
	}

	return best
}

// suggestTag proposes a corrected "TAG" or "TAG/SEVERITY" prefix for an
// unrecognized commit tag, or "" when nothing is close enough.
func (c CommitPolicyConfig) suggestTag(tag, severity string, patchTypes []string) string {
	tags := []string{}
	severities := []string{}
	for _, pType := range patchTypes {
		tags = append(tags, c.PatchTypes[pType].Values...)
		if scope := c.PatchTypes[pType].Scope; scope != "" {
			severities = append(severities, c.PatchScopes[scope]...)
		}
	}

	suggestion := closestMatch(strings.ToUpper(tag), tags)
	if suggestion == "" {
		return ""
	}

	original := tag
	if severity != "" {
		bestSeverity := closestMatch(strings.ToUpper(severity), severities)
		if bestSeverity == "" {
			return ""
		}
		suggestion += "/" + bestSeverity
		original += "/" + severity
	}

	if suggestion == original { // input already looks like the suggestion, nothing helpful to say
		return ""
	}

	return suggestion
}

// suggestFromSubject handles subjects where no tag pattern matched at all,
// e.g. lowercase tags; it inspects the text before the first colon.
func (c CommitPolicyConfig) suggestFromSubject(rawSubject []byte, patchTypes []string) string {
	prefix, _, found := strings.Cut(string(rawSubject), ":")
	if !found || prefix == "" || strings.ContainsAny(prefix, " \t") {
		return ""
	}

	tag, severity, _ := strings.Cut(prefix, "/")

	return c.suggestTag(tag, severity, patchTypes)
}

// suggestionHint formats a suggestion for inclusion in error messages.
func suggestionHint(suggestion string) string {
	if suggestion == "" {
		return ""
	}

	return fmt.Sprintf(" (did you mean '%s'?)", suggestion)
}
