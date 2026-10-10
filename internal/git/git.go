package git

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

func GetStagedFiles(ctx context.Context) ([]string, error) {
	return runGitLines(ctx, "diff", "--cached", "--name-only", "--diff-filter=ACMR")
}

func GetUncommittedFiles(ctx context.Context) ([]string, error) {
	return runGitLines(ctx, "diff", "--name-only", "--diff-filter=ACMR")
}

func GetAllTrackedFiles(ctx context.Context) ([]string, error) {
	return runGitLines(ctx, "ls-files")
}

func GetStagedFileContent(ctx context.Context, path string) (string, error) {
	// The leading ':' makes git show read the staged (index) copy, not HEAD.
	out, err := output(ctx, "show", ":"+path)
	if err != nil {
		return "", fmt.Errorf("failed to get staged content for %s: %w", path, err)
	}

	return string(out), nil
}

func GetStagedDiff(ctx context.Context, path string) (string, error) {
	out, err := output(ctx, "diff", "--cached", "--unified=100", "--", path)
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff for %s: %w", path, err)
	}

	return string(out), nil
}

func GetWorktreeDiff(ctx context.Context, path string) (string, error) {
	out, err := output(ctx, "diff", "--unified=100", "--", path)
	if err != nil {
		return "", fmt.Errorf("failed to get worktree diff for %s: %w", path, err)
	}

	return string(out), nil
}

func GetRepoRoot(ctx context.Context) (string, error) {
	out, err := output(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("failed to find git root (are you in a git repo?): %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// -z goes right after the subcommand (args[0]) so a caller's arguments can end in "--" and a pathspec.
func runGitLines(ctx context.Context, args ...string) ([]string, error) {
	out, err := output(ctx, slices.Insert(slices.Clone(args), 1, "-z")...)
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

// A killed git surfaces as a bare exit error; report the cancellation that caused it instead.
func output(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return out, err
}
