package git

import (
	"fmt"
	"os/exec"
	"strings"
)

func GetStagedFiles() ([]string, error) {
	return runGitLines("diff", "--cached", "--name-only", "--diff-filter=ACMR")
}

func GetUncommittedFiles() ([]string, error) {
	return runGitLines("diff", "--name-only", "--diff-filter=ACMR")
}

func GetAllTrackedFiles() ([]string, error) {
	return runGitLines("ls-files")
}

func GetStagedFileContent(path string) (string, error) {
	// The leading ':' makes git show read the staged (index) copy, not HEAD.
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
	cmd := exec.Command("git", "diff", "--unified=100", "--", path)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get worktree diff for %s: %w", path, err)
	}

	return string(out), nil
}

func GetRepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("failed to find git root (are you in a git repo?): %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func runGitLines(args ...string) ([]string, error) {
	cmd := exec.Command("git", append(args, "-z")...)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git command failed %v: %w", args, err)
	}

	var result []string
	for _, l := range strings.Split(string(out), "\x00") {
		if l != "" {
			result = append(result, l)
		}
	}

	return result, nil
}
