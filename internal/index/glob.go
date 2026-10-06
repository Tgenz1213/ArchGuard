package index

import "github.com/bmatcuk/doublestar/v4"

func MatchGlob(pattern, name string) bool {
	matched, err := doublestar.Match(pattern, name)
	if err != nil {
		return false
	}

	return matched
}
