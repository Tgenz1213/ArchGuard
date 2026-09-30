package cli

import (
	"bytes"
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
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/output"
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

func TestStageExitCodeValues(t *testing.T) {
	if ExitStageUnavailable != 6 || ExitStagePrecondition != 7 {
		t.Fatalf("stage exit codes = %d and %d, want 6 and 7", ExitStageUnavailable, ExitStagePrecondition)
	}
}

func TestStageExitCodesAreDistinctFromExistingCodes(t *testing.T) {
	seen := map[ExitCode]string{}
	for name, code := range map[string]ExitCode{
		"success": ExitSuccess, "error": ExitError, "usage": ExitUsage, "config": ExitConfig,
		"drift": ExitDriftDetected, "index": ExitIndexError,
		"unavailable": ExitStageUnavailable, "precondition": ExitStagePrecondition,
		"interrupted": ExitInterrupted,
	} {
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share exit code %d", name, other, code)
		}

		seen[code] = name
	}
}

func TestWriteCheckReport_FailuresOmittedWhenNone(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCheckReport(&buf, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), "failures") {
		t.Errorf("report %q should not mention failures when there are none", buf.String())
	}
}

func TestWriteCheckReport_IncludesFailureKind(t *testing.T) {
	var buf bytes.Buffer

	failures := []analysis.StageFailure{{Stage: "rank", File: "a.go", Kind: stage.KindUnavailable, Error: "down"}}
	if err := writeCheckReport(&buf, nil, nil, failures); err != nil {
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

func TestValidateFrontmatterMappings_RulesFieldCanBeRemapped(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"rules": "screening"}

	mappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mappings["rules"] != "screening" {
		t.Errorf("mappings[rules] = %q, want %q", mappings["rules"], "screening")
	}
}

func TestValidateFrontmatterMappings_MappedKeyCollidesWithRulesDefaultErrors(t *testing.T) {
	cfg := &config.Config{}

	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "rules"}

	_, err := validateFrontmatterMappings(cfg)
	if err == nil {
		t.Fatal("expected collision error when a mapped key matches the rules field's default key, got nil")
	}

	if want := `"rules" reads that key by default, so map "rules" to another key (e.g. rules: rules_field)`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to suggest %q", err, want)
	}
}

func TestValidateFrontmatterMappings_CollisionBetweenTwoMappedKeysHasNoRemapHint(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "x", "title": "x"}

	_, err := validateFrontmatterMappings(cfg)
	if err == nil || strings.Contains(err.Error(), "reads that key by default") {
		t.Fatalf("error = %v, want a collision error without the default-key hint", err)
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
			got := resolveContentProvider(output.New(os.Stdout, false), tt.files, tt.staged, tt.all, tt.updateBaseline)
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
				got = resolveContentProvider(output.New(os.Stdout, false), tt.files, false, false, false)
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

	claude, err := buildProvider(output.Discard(), "claude", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(claude) failed: %v", err)
	}

	if _, ok := claude.(*llm.ClaudeProvider); !ok {
		t.Errorf("expected *llm.ClaudeProvider, got %T", claude)
	}

	voyage, err := buildProvider(output.Discard(), "voyage", "test-key", cfg)
	if err != nil {
		t.Fatalf("buildProvider(voyage) failed: %v", err)
	}

	if _, ok := voyage.(*llm.VoyageProvider); !ok {
		t.Errorf("expected *llm.VoyageProvider, got %T", voyage)
	}
}

// Under --format json, stdout must carry only the JSON document.
func TestBuildProvider_MissingAPIKeyWarningRespectsWriter(t *testing.T) {
	cfg := &config.Config{LLM: config.LLMConfig{Model: "gpt-4"}}

	var buf bytes.Buffer
	if _, err := buildProvider(output.New(&buf, false), "openai", "", cfg); err != nil {
		t.Fatalf("buildProvider failed: %v", err)
	}

	if !strings.Contains(buf.String(), "no API key set") {
		t.Errorf("expected the missing-API-key warning on the given writer, got: %q", buf.String())
	}
}

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCommand string
		wantCode    ExitCode
		wantErr     bool
		wantOut     string
		check       func(t *testing.T, c checkCmd)
	}{
		{name: "check with paths", args: []string{"check", "a.go", "b.go"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if len(c.Paths) != 2 || c.Paths[0] != "a.go" || c.Paths[1] != "b.go" {
				t.Errorf("Paths = %v", c.Paths)
			}
		}},
		{name: "flags after paths are parsed", args: []string{"check", "a.go", "--debug"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if !c.Debug || len(c.Paths) != 1 {
				t.Errorf("Debug = %v, Paths = %v", c.Debug, c.Paths)
			}
		}},
		{name: "value flag is not a path", args: []string{"check", "--update-baseline", "--baseline-reason", "accepted-debt"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if c.BaselineReason != "accepted-debt" || len(c.Paths) != 0 {
				t.Errorf("BaselineReason = %q, Paths = %v", c.BaselineReason, c.Paths)
			}
		}},
		{name: "format json", args: []string{"check", "--format", "json"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if !c.jsonOutput() {
				t.Error("jsonOutput() = false, want true")
			}
		}},
		{name: "format json ignored with update-baseline", args: []string{"check", "--format=json", "--update-baseline"}, wantCommand: "check", check: func(t *testing.T, c checkCmd) {
			if c.jsonOutput() {
				t.Error("jsonOutput() = true, want false")
			}
		}},
		{name: "index", args: []string{"index"}, wantCommand: "index"},
		{name: "init", args: []string{"init"}, wantCommand: "init"},
		{name: "--help", args: []string{"--help"}, wantOut: "Usage: archguard <command>"},
		{name: "-h", args: []string{"-h"}, wantOut: "Usage: archguard <command>"},
		{name: "help", args: []string{"help"}, wantOut: "Usage: archguard <command>"},
		{name: "check --help after flags", args: []string{"check", "--format", "json", "--help"}, wantOut: "Scan staged files only"},
		{name: "index --help", args: []string{"index", "--help"}, wantOut: "Usage: archguard index"},
		{name: "--version", args: []string{"--version"}, wantOut: "ArchGuard version"},
		{name: "no command", args: nil, wantCode: ExitUsage, wantErr: true, wantOut: "Usage: archguard <command>"},
		{name: "unknown command", args: []string{"typo"}, wantCode: ExitUsage, wantErr: true},
		{name: "invalid format", args: []string{"check", "--format", "xml"}, wantCode: ExitUsage, wantErr: true},
		{name: "single-dash long flag", args: []string{"check", "-debug"}, wantCode: ExitUsage, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			inv, code, err := parseCommandLine(tt.args, &out)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}

			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output missing %q, got:\n%s", tt.wantOut, out.String())
			}

			if tt.wantCommand == "" {
				if inv != nil {
					t.Fatalf("expected the command line to be handled during parsing, got command %q", inv.command)
				}

				if code != tt.wantCode {
					t.Errorf("exit code = %d, want %d", code, tt.wantCode)
				}

				return
			}

			if inv == nil || inv.command != tt.wantCommand {
				t.Fatalf("invocation = %+v, want command %q", inv, tt.wantCommand)
			}

			if tt.check != nil {
				tt.check(t, inv.check)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestParseCommandLine_OutputWriteFailureExitsOne(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"check", "--help"}, {"--version"}, {"typo"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			inv, code, err := parseCommandLine(args, failingWriter{})
			if inv != nil || code != ExitError || err == nil {
				t.Fatalf("got (%v, %d, %v), want (nil, %d, error)", inv, code, err, ExitError)
			}
		})
	}
}

func TestNormalizePaths_CleansRelativePathAtRepoRoot(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())

	paths := []string{"./sub/../file.go"}
	normalizePaths(paths, repoRoot, repoRoot)

	if paths[0] != "file.go" {
		t.Errorf("got %q, want %q", paths[0], "file.go")
	}
}

func TestNormalizePaths_ResolvesFromSubdirectory(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())
	cwd := filepath.Join(repoRoot, "internal", "cli")

	paths := []string{"cli.go"}
	normalizePaths(paths, cwd, repoRoot)

	if paths[0] != "internal/cli/cli.go" {
		t.Errorf("got %q, want %q", paths[0], "internal/cli/cli.go")
	}
}

func TestNormalizePaths_HandlesAbsolutePath(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())

	paths := []string{filepath.Join(repoRoot, "sub", "file.go")}
	normalizePaths(paths, repoRoot, repoRoot)

	if paths[0] != "sub/file.go" {
		t.Errorf("got %q, want %q", paths[0], "sub/file.go")
	}
}

func TestNormalizePaths_LeavesEmptyPathUntouched(t *testing.T) {
	repoRoot := filepath.Clean(t.TempDir())

	paths := []string{""}
	normalizePaths(paths, repoRoot, repoRoot)

	if paths[0] != "" {
		t.Errorf("an empty path must stay empty, not become %q (a whole-repo scan), got %q", ".", paths[0])
	}
}

func TestNormalizePaths_MatchesBaselineEntryRecordedWithForwardSlashes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash-as-separator is a Windows-only path.filepath behavior")
	}

	repoRoot := filepath.Clean(t.TempDir())

	paths := []string{`internal\analysis\engine.go`}
	normalizePaths(paths, repoRoot, repoRoot)

	b := baseline.New()
	b.Add(baseline.Entry{ADRID: "0001", File: "internal/analysis/engine.go", QuotedCode: "quoted violating code"})

	if !b.IsSuppressed("0001", paths[0], "some context\nquoted violating code\nmore context") {
		t.Errorf("normalized path %q should match a baseline entry recorded with forward slashes", paths[0])
	}
}

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

func setupExecuteTestRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	gitInit := exec.CommandContext(t.Context(), "git", "init")

	gitInit.Dir = repoRoot
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git repo: %v\n%s", err, out)
	}

	resolvedRoot, err := exec.CommandContext(t.Context(), "git", "-C", repoRoot, "rev-parse", "--show-toplevel").Output()
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
			_, _ = Execute(t.Context(), ProviderFactories{})
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
			_, _ = Execute(t.Context(), ProviderFactories{})
		})
	})

	if !strings.Contains(stderr, "failed to load .env") {
		t.Errorf("expected a .env parse-failure warning on stderr, got: %q", stderr)
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
				exitCode, err = Execute(t.Context(), ProviderFactories{})
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

func TestPrintIndexSummary_MalformedRules(t *testing.T) {
	var buf bytes.Buffer

	printIndexSummary(index.BuildIndexResult{
		Discovered: 2,
		Valid:      2,
		MalformedRules: []index.MalformedRules{
			{RelPath: "0001-a.md", Reason: "frontmatter: rules must be a list"},
			{RelPath: "0002-b.md", Reason: `"Rules" section: rule 1: bullet has no statement text`},
		},
	}, output.New(&buf, false))

	want := "  Rules ignored (malformed): 2\n" +
		"    - 0001-a.md: frontmatter: rules must be a list\n" +
		"    - 0002-b.md: \"Rules\" section: rule 1: bullet has no statement text\n"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("summary missing the malformed-rules section.\ngot:\n%s\nwant it to contain:\n%s", buf.String(), want)
	}
}

func TestPrintIndexSummary_NoMalformedRulesSectionWhenNone(t *testing.T) {
	var buf bytes.Buffer

	printIndexSummary(index.BuildIndexResult{Discovered: 1, Valid: 1}, output.New(&buf, false))

	if strings.Contains(buf.String(), "Rules ignored") {
		t.Errorf("expected no malformed-rules section, got:\n%s", buf.String())
	}
}
