package analysis

import (
	"os"

	"github.com/tgenz1213/archguard/internal/git"
)

// ContentProvider abstracts how files and their content/diffs are retrieved.
type ContentProvider interface {
	GetFiles() ([]string, error)
	GetContent(path string) (string, error)
	GetDiff(path string) (string, error)
}

// UncommittedProvider scans files with worktree changes.
type UncommittedProvider struct{}

func (p *UncommittedProvider) GetFiles() ([]string, error) {
	return git.GetUncommittedFiles()
}

func (p *UncommittedProvider) GetContent(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *UncommittedProvider) GetDiff(path string) (string, error) {
	return git.GetWorktreeDiff(path)
}

// StagedProvider scans files currently in the git index.
type StagedProvider struct{}

func (p *StagedProvider) GetFiles() ([]string, error) {
	return git.GetStagedFiles()
}

func (p *StagedProvider) GetContent(path string) (string, error) {
	return git.GetStagedFileContent(path)
}

func (p *StagedProvider) GetDiff(path string) (string, error) {
	return git.GetStagedDiff(path)
}

// AllProvider scans all tracked files in the repository.
type AllProvider struct{}

func (p *AllProvider) GetFiles() ([]string, error) {
	return git.GetAllTrackedFiles()
}

func (p *AllProvider) GetContent(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *AllProvider) GetDiff(path string) (string, error) {
	return git.GetWorktreeDiff(path)
}

// MultiFileProvider scans a specific set of file paths from the worktree.
type MultiFileProvider struct{ Paths []string }

func (p *MultiFileProvider) GetFiles() ([]string, error) {
	return p.Paths, nil
}

func (p *MultiFileProvider) GetContent(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *MultiFileProvider) GetDiff(path string) (string, error) {
	return git.GetWorktreeDiff(path)
}
