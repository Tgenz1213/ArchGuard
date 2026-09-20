package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
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

func TestStageFailureExit(t *testing.T) {
	unavailable := analysis.StageFailure{Stage: "rank", File: "a.go", Kind: stage.KindUnavailable}
	precondition := analysis.StageFailure{Stage: "rank", File: "b.go", Kind: stage.KindPreconditionNotMet}
	tests := []struct {
		name     string
		failures []analysis.StageFailure
		want     ExitCode
	}{
		{"none", nil, ExitSuccess},
		{"unavailable only", []analysis.StageFailure{unavailable}, ExitStageUnavailable},
		{"precondition only", []analysis.StageFailure{precondition}, ExitStagePrecondition},
		{"both kinds", []analysis.StageFailure{unavailable, precondition, unavailable}, ExitStagePrecondition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := stageFailureExit(tt.failures)
			if code != tt.want || (err != nil) != (tt.want != ExitSuccess) {
				t.Fatalf("stageFailureExit = (%d, %v), want code %d", code, err, tt.want)
			}
		})
	}
}

func TestStageExitCodesAreDistinctFromExistingCodes(t *testing.T) {
	seen := map[ExitCode]string{}
	for name, code := range map[string]ExitCode{
		"success": ExitSuccess, "error": ExitError, "usage": ExitUsage, "config": ExitConfig,
		"drift": ExitDriftDetected, "index": ExitIndexError,
		"unavailable": ExitStageUnavailable, "precondition": ExitStagePrecondition,
	} {
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share exit code %d", name, other, code)
		}
		seen[code] = name
	}
}

func TestWriteCheckReport_FailuresOmittedWhenNone(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCheckReport(&buf, nil, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "failures") {
		t.Errorf("report %q should not mention failures when there are none", buf.String())
	}
}

func TestWriteCheckReport_IncludesFailureKind(t *testing.T) {
	var buf bytes.Buffer
	failures := []analysis.StageFailure{{Stage: "rank", File: "a.go", Kind: stage.KindUnavailable, Error: "down"}}
	if err := writeCheckReport(&buf, nil, failures); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Failures []map[string]string `json:"failures"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if len(got.Failures) != 1 || got.Failures[0]["kind"] != "unavailable" || got.Failures[0]["stage"] != "rank" || got.Failures[0]["file"] != "a.go" || got.Failures[0]["error"] != "down" {
		t.Errorf("failures = %v", got.Failures)
	}
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

func TestCompileADRIDPattern_EmptyIsNil(t *testing.T) {
	cfg := &config.Config{}
	re, err := compileADRIDPattern(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if re != nil {
		t.Errorf("re = %v, want nil", re)
	}
}

func TestCompileADRIDPattern_ValidPatternCompiles(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.ADRIDPattern = `^adr-(\d+)-`
	re, err := compileADRIDPattern(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if re == nil {
		t.Fatal("re = nil, want compiled pattern")
	}
}

func TestCompileADRIDPattern_InvalidPatternErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.ADRIDPattern = `[unterminated`
	_, err := compileADRIDPattern(cfg)
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

func TestValidateFrontmatterMappings_EmptyIsNil(t *testing.T) {
	cfg := &config.Config{}
	mappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mappings != nil {
		t.Errorf("mappings = %v, want nil", mappings)
	}
}

func TestValidateFrontmatterMappings_ValidMappingPassesThrough(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "applies_to"}
	mappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mappings["scope"] != "applies_to" {
		t.Errorf("mappings[scope] = %q, want %q", mappings["scope"], "applies_to")
	}
}

func TestValidateFrontmatterMappings_UnknownCanonicalFieldErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scop": "applies_to"}
	if _, err := validateFrontmatterMappings(cfg); err == nil {
		t.Fatal("expected error for unknown canonical field name, got nil")
	}
}

func TestValidateFrontmatterMappings_TwoFieldsMappedToSameKeyErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "x", "title": "x"}
	if _, err := validateFrontmatterMappings(cfg); err == nil {
		t.Fatal("expected collision error when two canonical fields map to the same YAML key, got nil")
	}
}

func TestValidateFrontmatterMappings_MappedKeyCollidesWithUnmappedDefaultErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "title"}
	if _, err := validateFrontmatterMappings(cfg); err == nil {
		t.Fatal("expected collision error when a mapped key matches an unmapped field's own default key, got nil")
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
			want:  &analysis.MultiFileProvider{Paths: []string{"internal/foo.go"}},
		},
		{
			name:  "multiple file args scan all of them",
			files: []string{"internal/foo.go", "internal/bar.go"},
			want:  &analysis.MultiFileProvider{Paths: []string{"internal/foo.go", "internal/bar.go"}},
		},
		{
			name:  "dot mixed with other file args still scans everything",
			files: []string{".", "internal/foo.go"},
			want:  &analysis.AllProvider{},
		},
		{
			name:  "dot as a non-first arg still scans everything",
			files: []string{"internal/foo.go", "."},
			want:  &analysis.AllProvider{},
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
			got := resolveContentProvider(os.Stdout, tt.files, tt.staged, tt.all, tt.updateBaseline)
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tt.want) {
				t.Fatalf("expected type %T, got %T", tt.want, got)
			}
			if mfp, ok := got.(*analysis.MultiFileProvider); ok {
				wantMFP := tt.want.(*analysis.MultiFileProvider)
				if !slices.Equal(mfp.Paths, wantMFP.Paths) {
					t.Errorf("expected paths %v, got %v", wantMFP.Paths, mfp.Paths)
				}
			}
		})
	}
}

func TestResolveContentProvider_DotMixedWithExtraArgsWarns(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{name: "dot first", files: []string{".", "internal/foo.go"}},
		{name: "dot not first", files: []string{"internal/foo.go", "."}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got analysis.ContentProvider
			output := captureStdout(t, func() {
				got = resolveContentProvider(os.Stdout, tt.files, false, false, false)
			})

			if _, ok := got.(*analysis.AllProvider); !ok {
				t.Fatalf("expected *analysis.AllProvider, got %T", got)
			}
			if !strings.Contains(output, "internal/foo.go") {
				t.Errorf("expected a warning naming the ignored extra argument %q, got output: %q", "internal/foo.go", output)
			}
		})
	}
}

func TestBuildProvider_ClaudeAndVoyage(t *testing.T) {
	cfg := &config.Config{
		LLM:         config.LLMConfig{Model: "claude-sonnet-4-5"},
		VectorStore: config.VectorStore{Model: "voyage-4"},
	}

	claude, err := buildProvider(os.Stdout, "claude", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(claude) failed: %v", err)
	}
	if _, ok := claude.(*llm.ClaudeProvider); !ok {
		t.Errorf("expected *llm.ClaudeProvider, got %T", claude)
	}

	voyage, err := buildProvider(os.Stdout, "voyage", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(voyage) failed: %v", err)
	}
	if _, ok := voyage.(*llm.VoyageProvider); !ok {
		t.Errorf("expected *llm.VoyageProvider, got %T", voyage)
	}
}

// TestBuildProvider_MissingAPIKeyWarningRespectsWriter guards --format
// json's stdout purity: a missing-API-key warning must go wherever the
// caller points it (stderr in JSON mode), not always to stdout.
func TestBuildProvider_MissingAPIKeyWarningRespectsWriter(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Model: "gpt-4"}}

	var buf bytes.Buffer
	if _, err := buildProvider(&buf, "openai", "", cfg); err != nil {
		t.Fatalf("buildProvider failed: %v", err)
	}

	if !strings.Contains(buf.String(), "no API key set") {
		t.Errorf("expected the missing-API-key warning on the given writer, got: %q", buf.String())
	}
}

func TestCheckWantsJSON(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "not check", args: []string{"archguard", "index", "--format", "json"}, want: false},
		{name: "text default", args: []string{"archguard", "check"}, want: false},
		{name: "format json space-separated", args: []string{"archguard", "check", "--format", "json"}, want: true},
		{name: "format=json", args: []string{"archguard", "check", "--format=json"}, want: true},
		{name: "format json with update-baseline", args: []string{"archguard", "check", "--format", "json", "--update-baseline"}, want: false},
		{name: "format=json with update-baseline=true", args: []string{"archguard", "check", "--format=json", "--update-baseline=true"}, want: false},
		{name: "format=json with update-baseline=false", args: []string{"archguard", "check", "--format=json", "--update-baseline=false"}, want: true},
		{name: "format text with update-baseline", args: []string{"archguard", "check", "--format", "text", "--update-baseline"}, want: false},
		{name: "baseline-reason value not mistaken for a flag", args: []string{"archguard", "check", "--baseline-reason", "--format", "--format", "json"}, want: true},
		{name: "format after positional stops parsing", args: []string{"archguard", "check", "a.go", "--format", "json"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkWantsJSON(tt.args); got != tt.want {
				t.Errorf("checkWantsJSON(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
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

func TestNormalizePositionalArgPaths_LeavesValueFlagArgumentUntouched(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())
	cwd := filepath.Join(repoRoot, "internal", "cli")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	args := []string{"archguard", "check", "--update-baseline", "--baseline-reason", "accepted-debt"}
	normalizePositionalArgPaths(args, cwd, repoRoot)

	if args[4] != "accepted-debt" {
		t.Errorf("expected --baseline-reason's value to be left untouched when run from a subdirectory, got %q (it was being mangled into a bogus repo-relative path derived from cwd, since it doesn't start with \"-\" and the rewrite couldn't tell a flag value from a positional file path)", args[4])
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
	b.Add(baseline.Entry{ADRID: "0001", File: "internal/analysis/engine.go", QuotedCode: "quoted violating code"})

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

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it. Mirrors captureStdout.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
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

// setupExecuteTestRepo creates a temp git repo, chdirs into it, and returns its resolved root.
func setupExecuteTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	gitInit := exec.Command("git", "init")
	gitInit.Dir = repoRoot
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

	resolvedRoot, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	cleanRoot := filepath.Clean(strings.TrimSpace(string(resolvedRoot)))

	if err := os.Chdir(cleanRoot); err != nil {
		t.Fatalf("failed to chdir into repo root: %v", err)
	}
	return cleanRoot
}

func TestExecute_MissingDotEnv_NoStderrWarning(t *testing.T) {
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

	setupExecuteTestRepo(t)

	os.Args = []string{"archguard", "check"}

	var stderr string
	captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			_, _ = Execute(ProviderFactories{})
		})
	})

	if stderr != "" {
		t.Errorf("expected empty stderr when .env is simply absent, got: %q", stderr)
	}
}

func TestExecute_MalformedDotEnv_PrintsStderrWarning(t *testing.T) {
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

	cleanRoot := setupExecuteTestRepo(t)

	if err := os.WriteFile(filepath.Join(cleanRoot, ".env"), []byte(`KEY="unterminated`), 0644); err != nil {
		t.Fatalf("failed to write malformed .env: %v", err)
	}

	os.Args = []string{"archguard", "check"}

	var stderr string
	captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			_, _ = Execute(ProviderFactories{})
		})
	})

	if !strings.Contains(stderr, "failed to load .env") {
		t.Errorf("expected a .env parse-failure warning on stderr, got: %q", stderr)
	}
}

func TestRunIndexCommand_HelpFlagExitsSuccess(t *testing.T) {
	cfg := &config.Config{}
	var exitCode ExitCode
	var runErr error
	output := captureStdout(t, func() {
		exitCode, runErr = runIndexCommand(context.Background(), cfg, nil, "", nil, nil, []string{"--help"})
	})

	if runErr != nil {
		t.Fatalf("expected no error, got %v", runErr)
	}
	if exitCode != ExitSuccess {
		t.Fatalf("expected exit code %d, got %d", ExitSuccess, exitCode)
	}
	if !strings.Contains(output, "Usage: archguard index") {
		t.Fatalf("expected index usage output, got %q", output)
	}
}

func TestIsTopLevelHelpRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "--help", args: []string{"archguard", "--help"}, want: true},
		{name: "-h", args: []string{"archguard", "-h"}, want: true},
		{name: "help", args: []string{"archguard", "help"}, want: true},
		{name: "no args", args: []string{"archguard"}, want: false},
		{name: "check subcommand", args: []string{"archguard", "check"}, want: false},
		{name: "check --help is not top-level help", args: []string{"archguard", "check", "--help"}, want: false},
		{name: "unknown command", args: []string{"archguard", "typo"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTopLevelHelpRequest(tt.args); got != tt.want {
				t.Errorf("isTopLevelHelpRequest(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestSubcommandHelpRequest(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantSubcmd string
		wantOK     bool
	}{
		{name: "check --help", args: []string{"archguard", "check", "--help"}, wantSubcmd: "check", wantOK: true},
		{name: "check -h", args: []string{"archguard", "check", "-h"}, wantSubcmd: "check", wantOK: true},
		{name: "index --help", args: []string{"archguard", "index", "--help"}, wantSubcmd: "index", wantOK: true},
		{name: "index -h", args: []string{"archguard", "index", "-h"}, wantSubcmd: "index", wantOK: true},
		{name: "check --help after other flags", args: []string{"archguard", "check", "--debug", "--help"}, wantSubcmd: "check", wantOK: true},
		{name: "check with no help", args: []string{"archguard", "check", "--debug"}, wantSubcmd: "", wantOK: false},
		{name: "help stops at first positional arg", args: []string{"archguard", "check", "foo.go", "--help"}, wantSubcmd: "", wantOK: false},
		{name: "help detected after a value-taking flag", args: []string{"archguard", "check", "--format", "json", "--help"}, wantSubcmd: "check", wantOK: true},
		{name: "init is not a help-eligible subcommand", args: []string{"archguard", "init", "--help"}, wantSubcmd: "", wantOK: false},
		{name: "no args", args: []string{"archguard"}, wantSubcmd: "", wantOK: false},
		{name: "top-level help is not subcommand help", args: []string{"archguard", "--help"}, wantSubcmd: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSubcmd, gotOK := subcommandHelpRequest(tt.args)
			if gotSubcmd != tt.wantSubcmd || gotOK != tt.wantOK {
				t.Errorf("subcommandHelpRequest(%v) = (%q, %v), want (%q, %v)", tt.args, gotSubcmd, gotOK, tt.wantSubcmd, tt.wantOK)
			}
		})
	}
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

	// Confirms this test actually exercises cwd == repoRoot, the same way
	// Execute computes and compares them -- otherwise a path-canonicalization
	// difference could silently degrade this into testing the wrong branch.
	gotWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory after chdir: %v", err)
	}
	if !strings.EqualFold(filepath.Clean(gotWd), cleanRoot) {
		t.Fatalf("precondition failed: cwd %q does not equal repoRoot %q", gotWd, cleanRoot)
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

func TestExecute_TopLevelHelpExitsSuccess(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, help := range []string{"--help", "-h", "help"} {
		t.Run(help, func(t *testing.T) {
			os.Args = []string{"archguard", help}
			var exitCode ExitCode
			var err error
			output := captureStdout(t, func() {
				exitCode, err = Execute(ProviderFactories{})
			})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if exitCode != ExitSuccess {
				t.Fatalf("expected exit code %d, got %d", ExitSuccess, exitCode)
			}
			if !strings.Contains(output, "Usage: archguard") {
				t.Fatalf("expected usage output, got %q", output)
			}
		})
	}
}

func TestRunCheck_HelpFlagExitsSuccessWithCustomUsage(t *testing.T) {
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(origWd); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	}()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}

	cfg := &config.Config{}
	var exitCode ExitCode
	var runErr error
	output := captureStdout(t, func() {
		exitCode, runErr = runCheck(cfg, nil, nil, "", nil, nil, []string{"--help"})
	})

	if runErr != nil {
		t.Fatalf("expected no error, got %v", runErr)
	}
	if exitCode != ExitSuccess {
		t.Fatalf("expected exit code %d, got %d", ExitSuccess, exitCode)
	}
	if !strings.Contains(output, "--staged") || !strings.Contains(output, "Scan staged files only") {
		t.Fatalf("expected flag descriptions in help output, got %q", output)
	}
	if strings.Contains(output, "Usage of check:") {
		t.Fatalf("expected custom usage, not Go's default flag.PrintDefaults() output; got %q", output)
	}
}
