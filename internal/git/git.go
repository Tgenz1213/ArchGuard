package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetStagedFiles returns files with changes in the index
func GetStagedFiles() ([]string, error) {
	return runGitLines("diff", "--cached", "--name-only", "--diff-filter=ACMR")
}

// GetUncommittedFiles returns files with changes in the worktree relative to index
func GetUncommittedFiles() ([]string, error) {
	return runGitLines("diff", "--name-only", "--diff-filter=ACMR")
}

// GetAllTrackedFiles returns all files tracked by git
func GetAllTrackedFiles() ([]string, error) {
	return runGitLines("ls-files")
}

func GetStagedFileContent(path string) (string, error) {
	// git show :path/to/file gets the staged content
	// Note: relative paths must be correct.
	cmd := exec.Command("git", "show", ":"+path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged content for %s: %w", path, err)
	}
	return string(out), nil
}

func GetStagedDiff(path string) (string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--unified=100", "--", path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff for %s: %w", path, err)
	}
	return string(out), nil
}

func GetWorktreeDiff(path string) (string, error) {
	// Diff worktree against index
	cmd := exec.Command("git", "diff", "--unified=100", "--", path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree diff for %s: %w", path, err)
	}
	return string(out), nil
}

// GetRepoRoot returns the absolute path to the git repository root
func GetRepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("failed to find git root (are you in a git repo?): %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runGitLines(args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git command failed %v: %w", args, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result, nil
}
