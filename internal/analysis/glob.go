package analysis

import (
	"path/filepath"
	"regexp"
	"strings"
)

// matchGlob matches a file name against a glob pattern, supporting standard
// filepath.Match patterns as well as recursive double-star (**) patterns.
func matchGlob(pattern, name string) bool {
	if !strings.Contains(pattern, "**") {
		m, _ := filepath.Match(pattern, name)
		return m
	}

	return matchRegexGlob(pattern, name)
}

// matchRegexGlob converts a glob pattern containing double-stars into a
// regular expression to perform recursive path matching.
func matchRegexGlob(pattern, name string) bool {
	var reStr strings.Builder
	reStr.WriteString("^")

	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				reStr.WriteString(".*")
				i++
			} else {
				reStr.WriteString("[^/]*")
			}
		case '?':
			reStr.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '[', ']', '{', '}', '\\':
			reStr.WriteString("\\")
			reStr.WriteByte(c)
		default:
			reStr.WriteByte(c)
		}
	}
	reStr.WriteString("$")

	matched, _ := regexp.MatchString(reStr.String(), name)
	return matched
}
