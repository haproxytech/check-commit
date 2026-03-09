package match

import (
	"path/filepath"
	"regexp"
	"strings"
)

// MatchFilter checks if a file matches a filter.
//
// Parameters:
//   - file: the file to be checked.
//   - filter: the filter to be matched against the file.
//
// Returns:
//   - bool: true if the file matches the filter, false otherwise.
//   - error: an error if there was a problem during the matching process.
func MatchFilter(file, filter string) bool {
	if filter == "" {
		return false
	}

	filter = strings.TrimLeft(filter, "/")
	file = strings.TrimLeft(file, "/")

	if strings.Contains(filter, "*") {
		matched, err := filepath.Match(filter, file)
		if err != nil {
			return false
		}
		return matchFilterRegex(file, filter) || matched
	}

	return file == filter
}

func matchFilterRegex(file, filter string) bool {
	filter = strings.Replace(filter, "*", ".*", -1)
	re := regexp.MustCompile(filter)
	return re.MatchString(file)
}
