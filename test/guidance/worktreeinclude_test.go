package guidance

import (
	"path"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

var envFiles = []string{".env", ".env.local", "internal/cli/.env"}

func TestWorktreeIncludeCopiesNoEnvFile(t *testing.T) {
	for _, pattern := range worktreeIncludePatterns(t) {
		for _, file := range envFiles {
			if matchesGitignorePattern(pattern, file) {
				t.Errorf(".worktreeinclude pattern %q copies %s into new worktrees", pattern, file)
			}
		}
	}
}

func worktreeIncludePatterns(t *testing.T) []string {
	t.Helper()

	var patterns []string

	for _, line := range strings.Split(readGuidance(t, ".worktreeinclude"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		patterns = append(patterns, line)
	}

	return patterns
}

// matchesGitignorePattern reports whether a .gitignore-style pattern selects file.
// A pattern with no slash before its end matches at any depth.
func matchesGitignorePattern(pattern, file string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	if !strings.Contains(pattern, "/") {
		return doublestar.MatchUnvalidated(pattern, path.Base(file))
	}

	return doublestar.MatchUnvalidated(strings.TrimPrefix(pattern, "/"), file)
}
