package analysis

import (
	"context"
	"os"

	"github.com/tgenz1213/archguard/internal/git"
)

type ContentProvider interface {
	GetFiles(ctx context.Context) ([]string, error)
	GetContent(ctx context.Context, path string) (string, error)
	GetDiff(ctx context.Context, path string) (string, error)
}

type UncommittedProvider struct{}

func (p *UncommittedProvider) GetFiles(ctx context.Context) ([]string, error) {
	return git.GetUncommittedFiles(ctx)
}

func (p *UncommittedProvider) GetContent(_ context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (p *UncommittedProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return git.GetWorktreeDiff(ctx, path)
}

type StagedProvider struct{}

func (p *StagedProvider) GetFiles(ctx context.Context) ([]string, error) {
	return git.GetStagedFiles(ctx)
}

func (p *StagedProvider) GetContent(ctx context.Context, path string) (string, error) {
	return git.GetStagedFileContent(ctx, path)
}

func (p *StagedProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return git.GetStagedDiff(ctx, path)
}

type AllProvider struct{}

func (p *AllProvider) GetFiles(ctx context.Context) ([]string, error) {
	return git.GetAllTrackedFiles(ctx)
}

func (p *AllProvider) GetContent(_ context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (p *AllProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return git.GetWorktreeDiff(ctx, path)
}

type ChangedSinceProvider struct{ Ref string }

func (p *ChangedSinceProvider) GetFiles(ctx context.Context) ([]string, error) {
	return git.GetFilesChangedSince(ctx, p.Ref)
}

func (p *ChangedSinceProvider) GetContent(_ context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (p *ChangedSinceProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return git.GetDiffSince(ctx, p.Ref, path)
}

type MultiFileProvider struct{ Paths []string }

func (p *MultiFileProvider) GetFiles(_ context.Context) ([]string, error) {
	seen := make(map[string]struct{}, len(p.Paths))
	files := make([]string, 0, len(p.Paths))
	for _, path := range p.Paths {
		if _, ok := seen[path]; ok {
			continue
		}

		seen[path] = struct{}{}
		files = append(files, path)
	}

	return files, nil
}

func (p *MultiFileProvider) GetContent(_ context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func (p *MultiFileProvider) GetDiff(ctx context.Context, path string) (string, error) {
	return git.GetWorktreeDiff(ctx, path)
}
