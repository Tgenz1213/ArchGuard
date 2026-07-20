package analysis

import "github.com/bmatcuk/doublestar/v4"

// matchGlob matches a file path against a glob pattern, supporting standard
// single-segment wildcards as well as recursive double-star (**) patterns.
func matchGlob(pattern, name string) bool {
	matched, err := doublestar.Match(pattern, name)
	if err != nil {
		return false
	}
	return matched
}
