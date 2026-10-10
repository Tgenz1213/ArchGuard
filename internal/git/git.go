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

type ChangedFile struct {
	Path string
	// Set when the file was renamed, so its diff can be taken against the old path.
	OldPath string
}

func GetChangedSince(ctx context.Context, ref string) ([]ChangedFile, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	out, err := output(ctx, "diff", "-z", "--name-status", "-M", "--diff-filter=ACMR", ref, "HEAD", "--")
	if err != nil {
		return nil, fmt.Errorf("failed to list files changed since %q: %w", ref, err)
	}

	return parseNameStatus(strings.Split(string(out), "\x00")), nil
}

func parseNameStatus(fields []string) []ChangedFile {
	var files []ChangedFile

	for i := 0; i < len(fields); {
		status := fields[i]

		switch {
		case status == "":
			i++
		case (status[0] == 'R' || status[0] == 'C') && i+2 < len(fields):
			files = append(files, ChangedFile{Path: fields[i+2], OldPath: fields[i+1]})
			i += 3
		case i+1 < len(fields):
			files = append(files, ChangedFile{Path: fields[i+1]})
			i += 2
		default:
			i++
		}
	}

	return files
}

func GetDiffSince(ctx context.Context, ref string, paths ...string) (string, error) {
	if err := validateRef(ref); err != nil {
		return "", err
	}

	args := append([]string{"diff", "-M", "--unified=100", ref, "HEAD", "--"}, paths...)

	out, err := output(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("failed to get diff for %v since %q: %w", paths, ref, err)
	}

	return string(out), nil
}

func validateRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("invalid git ref %q", ref)
	}

	return nil
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
	return out, err
}
