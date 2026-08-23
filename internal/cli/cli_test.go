package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/llm"
)

func TestExitCodeForAnalysisError(t *testing.T) {
	t.Run("returns drift exit code for direct drift detection errors", func(t *testing.T) {
		err := &analysis.DriftDetectedError{Count: 2}
		if got := exitCodeForAnalysisError(err); got != ExitDriftDetected {
			t.Fatalf("expected %d, got %d", ExitDriftDetected, got)
		}
	})

	t.Run("returns drift exit code for wrapped drift detection errors", func(t *testing.T) {
		err := fmt.Errorf("wrapped: %w", &analysis.DriftDetectedError{Count: 2})
		if got := exitCodeForAnalysisError(err); got != ExitDriftDetected {
			t.Fatalf("expected %d, got %d", ExitDriftDetected, got)
		}
	})

	t.Run("returns generic error exit code for operational errors", func(t *testing.T) {
		err := errors.New("git content provider failure")
		if got := exitCodeForAnalysisError(err); got != ExitError {
			t.Fatalf("expected %d, got %d", ExitError, got)
		}
	})
}

func TestValidateProviderConfig_ClaudeRequiresEmbeddingProvider(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: ""},
	}
	if err := validateProviderConfig(cfg); err == nil {
		t.Fatal("expected an error when llm.provider is claude and vector_store.provider is unset")
	}
}

func TestValidateProviderConfig_ClaudeWithEmbeddingProviderOK(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	if err := validateProviderConfig(cfg); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateProviderConfig_NonClaudeProvidersUnaffected(t *testing.T) {
	for _, provider := range []string{"openai", "ollama", "gemini"} {
		cfg := &config.Config{
			LLM:         config.LLMConfig{Provider: provider},
			VectorStore: config.VectorStore{Provider: ""},
		}
		if err := validateProviderConfig(cfg); err != nil {
			t.Errorf("provider %q: expected no error with vector_store.provider unset, got: %v", provider, err)
		}
	}
}

func TestValidateProviderConfig_VoyageRejectedAsLLMProvider(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{Provider: "voyage"},
	}
	if err := validateProviderConfig(cfg); err == nil {
		t.Fatal("expected an error when llm.provider is voyage (embeddings-only, no chat capability)")
	}
}

func TestValidateProviderConfig_ClaudeRejectedAsEmbeddingProvider(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "gemini"},
		VectorStore: config.VectorStore{Provider: "claude"},
	}
	if err := validateProviderConfig(cfg); err == nil {
		t.Fatal("expected an error when vector_store.provider is claude (chat-only, no embeddings capability)")
	}
}

func TestResolveEmbedProvider_SameProviderReusesInstance(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "openai"},
		VectorStore: config.VectorStore{Provider: ""},
	}
	name, _, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if !reuse {
		t.Error("expected reuse=true when vector_store.provider is unset")
	}
	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
}

func TestResolveEmbedProvider_ExplicitSameProviderReusesInstance(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "openai"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	_, _, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if !reuse {
		t.Error("expected reuse=true when vector_store.provider explicitly matches llm.provider")
	}
}

func TestResolveEmbedProvider_DifferentProviderUsesEmbedKey(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	name, apiKey, reuse := resolveEmbedProvider(cfg, "chat-key", "embed-key")
	if reuse {
		t.Error("expected reuse=false for different providers")
	}
	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
	if apiKey != "embed-key" {
		t.Errorf("expected embed-key, got %q", apiKey)
	}
}

// TestResolveEmbedProvider_DifferentProviderNeverFallsBackToChatKey asserts
// an unset embed API key never falls back to the chat provider's key.
func TestResolveEmbedProvider_DifferentProviderNeverFallsBackToChatKey(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	name, apiKey, reuse := resolveEmbedProvider(cfg, "chat-key", "")
	if reuse {
		t.Error("expected reuse=false for different providers")
	}
	if name != "openai" {
		t.Errorf("expected name openai, got %q", name)
	}
	if apiKey == "chat-key" {
		t.Fatal("REGRESSION: embed provider fell back to the chat provider's API key -- this is the exact credential-leak bug fixed in fee5a7c")
	}
	if apiKey != "" {
		t.Errorf("expected empty apiKey (embed key was unset, must not substitute chat key), got %q", apiKey)
	}
}

func TestResolveEmbedProviderInstance_ReusesChatProviderWhenNamesMatch(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Provider: "openai"}}
	chat := &llm.MockProvider{}

	got, err := resolveEmbedProviderInstance(cfg, chat, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != llm.Provider(chat) {
		t.Error("expected the chat provider instance to be reused")
	}
}

func TestResolveEmbedProviderInstance_BuildsFromFactoryWhenNamesDiffer(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	chat := &llm.MockProvider{}
	embed := &llm.MockProvider{}

	got, err := resolveEmbedProviderInstance(cfg, chat, func(*config.Config) llm.Provider { return embed })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != llm.Provider(embed) {
		t.Error("expected the embed factory's provider to be used, not the chat provider")
	}
}

func TestResolveEmbedProviderInstance_ErrorsWhenEmbedFactoryRequiredButNil(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Provider: "claude"},
		VectorStore: config.VectorStore{Provider: "openai"},
	}
	chat := &llm.MockProvider{}

	if _, err := resolveEmbedProviderInstance(cfg, chat, nil); err == nil {
		t.Fatal("expected an error when the roles need different providers but embedFactory is nil")
	}
}

func TestResolveContentProvider(t *testing.T) {
	tests := []struct {
		name           string
		files          []string
		staged         bool
		all            bool
		updateBaseline bool
		want           analysis.ContentProvider
	}{
		{
			name: "no args or flags defaults to uncommitted",
			want: &analysis.UncommittedProvider{},
		},
		{
			name:  "dot positional arg scans everything",
			files: []string{"."},
			want:  &analysis.AllProvider{},
		},
		{
			name:  "specific file arg scans just that file",
			files: []string{"internal/foo.go"},
			want:  &analysis.SingleFileProvider{Path: "internal/foo.go"},
		},
		{
			name:   "staged flag scans staged files",
			staged: true,
			want:   &analysis.StagedProvider{},
		},
		{
			name: "all flag scans all tracked files",
			all:  true,
			want: &analysis.AllProvider{},
		},
		{
			name:           "update-baseline alone scans everything",
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:           "update-baseline overrides staged",
			staged:         true,
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:           "update-baseline overrides a file arg",
			files:          []string{"internal/foo.go"},
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveContentProvider(tt.files, tt.staged, tt.all, tt.updateBaseline)
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tt.want) {
				t.Fatalf("expected type %T, got %T", tt.want, got)
			}
			if sfp, ok := got.(*analysis.SingleFileProvider); ok {
				wantSFP := tt.want.(*analysis.SingleFileProvider)
				if sfp.Path != wantSFP.Path {
					t.Errorf("expected path %q, got %q", wantSFP.Path, sfp.Path)
				}
			}
		})
	}
}

func TestBuildProvider_ClaudeAndVoyage(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Model: "claude-sonnet-4-5"},
		VectorStore: config.VectorStore{Model: "voyage-4"},
	}

	claude, err := buildProvider("claude", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(claude) failed: %v", err)
	}
	if _, ok := claude.(*llm.ClaudeProvider); !ok {
		t.Errorf("expected *llm.ClaudeProvider, got %T", claude)
	}

	voyage, err := buildProvider("voyage", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(voyage) failed: %v", err)
	}
	if _, ok := voyage.(*llm.VoyageProvider); !ok {
		t.Errorf("expected *llm.VoyageProvider, got %T", voyage)
	}
}

func TestNormalizePositionalArgPaths_RunsEvenWhenCwdEqualsRepoRoot(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())
	cwd := repoRoot

	args := []string{"archguard", "check", "./sub/../file.go", "--debug"}
	normalizePositionalArgPaths(args, cwd, repoRoot)

	if args[2] != "file.go" {
		t.Errorf("expected the uncleaned positional path to be cleaned to %q (proving the rewrite actually ran at cwd == repoRoot), got %q", "file.go", args[2])
	}
	if args[3] != "--debug" {
		t.Errorf("flag argument must be left untouched, got %q", args[3])
	}
}

func TestNormalizePositionalArgPaths_HandlesAbsolutePathArg(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())
	cwd := repoRoot

	absArg := filepath.Join(repoRoot, "sub", "file.go")
	args := []string{"archguard", "check", absArg}
	normalizePositionalArgPaths(args, cwd, repoRoot)

	want := filepath.ToSlash(filepath.Join("sub", "file.go"))
	if args[2] != want {
		t.Errorf("expected an absolute in-repo path arg to normalize to the repo-relative form %q, got %q (this is the case that broke: filepath.Join(cwd, arg) mangles an already-absolute arg instead of using it directly)", want, args[2])
	}
}

func TestNormalizePositionalArgPaths_LeavesEmptyArgUntouched(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())
	cwd := repoRoot

	args := []string{"archguard", "check", ""}
	normalizePositionalArgPaths(args, cwd, repoRoot)

	if args[2] != "" {
		t.Errorf("expected an empty positional arg to be left untouched (not resolved to %q, which resolveContentProvider treats as a whole-repo scan), got %q", ".", args[2])
	}
}

func TestNormalizePositionalArgPaths_ConvertsBackslashesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash-as-separator is a Windows-only path.filepath behavior")
	}

	repoRoot := filepath.Clean(t.TempDir())
	cwd := repoRoot

	args := []string{"archguard", "check", `internal\analysis\engine.go`}
	normalizePositionalArgPaths(args, cwd, repoRoot)

	if args[2] != "internal/analysis/engine.go" {
		t.Errorf("expected backslash-style arg to normalize to forward slashes at cwd == repoRoot, got %q", args[2])
	}
}

func TestNormalizePositionalArgPaths_MatchesBaselineEntryRecordedWithForwardSlashes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash-as-separator is a Windows-only path.filepath behavior")
	}

	repoRoot := filepath.Clean(t.TempDir())
	cwd := repoRoot

	args := []string{"archguard", "check", `internal\analysis\engine.go`}
	normalizePositionalArgPaths(args, cwd, repoRoot)

	b := baseline.New()
	b.Add("0001", "internal/analysis/engine.go", "quoted violating code")

	if !b.IsSuppressed("0001", args[2], "some context\nquoted violating code\nmore context") {
		t.Errorf("expected the normalized path %q to match a baseline entry recorded with forward slashes, but IsSuppressed returned false", args[2])
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it. Mirrors internal/analysis's helper of the same name.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read pipe: %v", err)
	}
	return buf.String()
}

// TestExecute_NormalizesPositionalArgPath_EvenWhenCwdEqualsRepoRoot pins
// Execute's call site, not just the extracted function, to running unconditionally.
func TestExecute_NormalizesPositionalArgPath_EvenWhenCwdEqualsRepoRoot(t *testing.T) {
	origArgs := os.Args
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original working directory: %v", err)
	}
	defer func() {
		os.Args = origArgs
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	}()

	repoRoot := t.TempDir()
	gitInit := exec.Command("git", "init")
	gitInit.Dir = repoRoot
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

	// Resolve the same way Execute's git.GetRepoRoot() does, so cwd == repoRoot
	// stays exact even where TMPDIR is a symlink.
	resolvedRoot, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	cleanRoot := filepath.Clean(strings.TrimSpace(string(resolvedRoot)))

	if err := os.Chdir(cleanRoot); err != nil {
		t.Fatalf("failed to chdir into repo root: %v", err)
	}
	// Avoids a "failed to load .env" stderr warning from godotenv.Load,
	// unrelated to what this test is checking.
	if err := os.WriteFile(filepath.Join(cleanRoot, ".env"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty .env: %v", err)
	}

	os.Args = []string{"archguard", "check", "./sub/../file.go"}

	// Execute fails shortly after (no archguard.yaml here) -- irrelevant,
	// since os.Args is already mutated by then.
	captureStdout(t, func() {
		_, _ = Execute(ProviderFactories{})
	})

	if os.Args[2] != "file.go" {
		t.Errorf("expected the uncleaned positional path to be normalized to %q by Execute itself even though cwd == repoRoot, got %q", "file.go", os.Args[2])
	}
}
