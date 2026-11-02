//go:build e2e

package e2e

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

type chatUsage struct {
	InputTokens  *int     `json:"input_tokens"`
	OutputTokens *int     `json:"output_tokens"`
	Cost         *float64 `json:"estimated_cost"`
	Calls        int      `json:"calls"`
	CacheHits    int      `json:"cache_hits"`
	CacheMisses  int      `json:"cache_misses"`
	Retries      int      `json:"retries"`
}

type usageReport struct {
	Usage struct {
		Judgment   chatUsage  `json:"judgment"`
		Suggestion *chatUsage `json:"suggestion"`
		Embedding  struct {
			Tokens *int     `json:"tokens"`
			Cost   *float64 `json:"estimated_cost"`
			Calls  int      `json:"calls"`
		} `json:"embedding"`
		Files []struct {
			File       string `json:"file"`
			DurationMS *int64 `json:"duration_ms"`
		} `json:"files"`
	} `json:"usage"`
}

func setupUsageRepo(t *testing.T) (dir, binaryPath string) {
	t.Helper()

	dir, binaryPath = buildE2EBinary(t)

	writeE2EConfig(t, dir, `
version: "1"
llm:
  provider: "ollama"
  cost_per_1k_input_tokens: 0.5
  cost_per_1k_output_tokens: 2
vector_store:
  provider: "ollama"
  embedding_dim: 768
  cost_per_1k_tokens: 0.1
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`+defaultPipelineYAML)
	writeNoSecretsADR(t, dir)

	if err := os.WriteFile(filepath.Join(dir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	return dir, binaryPath
}

func runUsageCheck(t *testing.T, dir, binaryPath string) usageReport {
	t.Helper()

	stdout, stderr, exitCode := runCheckWithEnv(t, dir, binaryPath, nil, "--format", "json", "--suggest-fixes", fixtureFilename)
	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code = %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
	}

	var report usageReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
	}

	return report
}

func assertChatTokens(t *testing.T, role string, got *chatUsage, wantCalls int) {
	t.Helper()

	if got == nil || got.InputTokens == nil || got.OutputTokens == nil || got.Cost == nil {
		t.Fatalf("%s usage = %+v, want every figure present", role, got)
	}

	wantInput, wantOutput := wantCalls*testutil.MockChatInputTokens, wantCalls*testutil.MockChatOutputTokens
	if *got.InputTokens != wantInput || *got.OutputTokens != wantOutput {
		t.Errorf("%s tokens = %d input, %d output, want %d, %d", role, *got.InputTokens, *got.OutputTokens, wantInput, wantOutput)
	}

	wantCost := float64(wantInput)/1000*0.5 + float64(wantOutput)/1000*2
	if math.Abs(*got.Cost-wantCost) > 1e-9 {
		t.Errorf("%s cost = %v, want %v", role, *got.Cost, wantCost)
	}
}

type chatCounts struct {
	calls, hits, misses int
}

func assertChatCounts(t *testing.T, role string, got *chatUsage, want chatCounts) {
	t.Helper()

	if got.Calls != want.calls || got.CacheHits != want.hits || got.CacheMisses != want.misses || got.Retries != 0 {
		t.Errorf("%s counts = %d calls, %d hits, %d misses, %d retries, want %+v and no retries", role, got.Calls, got.CacheHits, got.CacheMisses, got.Retries, want)
	}
}

func TestE2E_CheckReportsTokenUsage(t *testing.T) {
	dir, binaryPath := setupUsageRepo(t)

	t.Run("first run pays for one judgment, one suggestion and one query embedding", func(t *testing.T) {
		report := runUsageCheck(t, dir, binaryPath)

		assertChatTokens(t, "judgment", &report.Usage.Judgment, 1)
		assertChatTokens(t, "suggestion", report.Usage.Suggestion, 1)

		assertChatCounts(t, "judgment", &report.Usage.Judgment, chatCounts{calls: 1, misses: 1})
		assertChatCounts(t, "suggestion", report.Usage.Suggestion, chatCounts{calls: 1, misses: 1})

		embedding := report.Usage.Embedding
		if embedding.Tokens == nil || *embedding.Tokens != testutil.MockEmbedTokens || embedding.Calls != 1 {
			t.Errorf("embedding = %v tokens in %d call(s), want %d in 1", embedding.Tokens, embedding.Calls, testutil.MockEmbedTokens)
		}

		files := report.Usage.Files
		if len(files) != 1 || files[0].File != fixtureFilename || files[0].DurationMS == nil {
			t.Errorf("files = %+v, want one entry for %s with a duration", files, fixtureFilename)
		}
	})

	t.Run("cache hits add no chat tokens", func(t *testing.T) {
		report := runUsageCheck(t, dir, binaryPath)

		assertChatTokens(t, "judgment", &report.Usage.Judgment, 0)
		assertChatTokens(t, "suggestion", report.Usage.Suggestion, 0)
		assertChatCounts(t, "judgment", &report.Usage.Judgment, chatCounts{hits: 1})
		assertChatCounts(t, "suggestion", report.Usage.Suggestion, chatCounts{hits: 1})
	})
}

type dryRunReport struct {
	usageReport
	Count  int `json:"count"`
	DryRun *struct {
		Calls  int `json:"judgment_calls"`
		Cached int `json:"cached"`
	} `json:"dry_run"`
}

func TestE2E_DryRun(t *testing.T) {
	dir, binaryPath := setupUsageRepo(t)

	t.Run("projects the judgment without making it", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, dir, binaryPath, nil, "--dry-run", "--suggest-fixes", "--format", "json", fixtureFilename)
		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code = %d, want %d although the fixture violates. stderr: %s", exitCode, cli.ExitSuccess, stderr)
		}

		var report dryRunReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
		}

		if report.Count != 0 || report.DryRun == nil || report.DryRun.Calls != 1 || report.DryRun.Cached != 0 {
			t.Fatalf("count = %d, dry_run = %+v, want no violations and one projected call", report.Count, report.DryRun)
		}

		judgment := report.Usage.Judgment
		if judgment.InputTokens == nil || *judgment.InputTokens == 0 || judgment.OutputTokens != nil || judgment.Cost == nil {
			t.Errorf("judgment usage = %+v, want projected input tokens priced, and no output", judgment)
		}

		if report.Usage.Suggestion != nil {
			t.Errorf("suggestion usage = %+v, want none under a dry run", report.Usage.Suggestion)
		}

		if tokens := report.Usage.Embedding.Tokens; tokens == nil || *tokens != testutil.MockEmbedTokens {
			t.Errorf("embedding tokens = %v, want the real query embedding's %d", tokens, testutil.MockEmbedTokens)
		}
	})

	t.Run("writes no cache entry", func(t *testing.T) {
		entries, err := filepath.Glob(filepath.Join(dir, ".archguard", "cache", "*.json"))
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) != 0 {
			t.Errorf("a dry run wrote %d cache entries", len(entries))
		}
	})

	t.Run("text output exits 0", func(t *testing.T) {
		if _, stderr, exitCode := runCheckWithEnv(t, dir, binaryPath, nil, "--dry-run", fixtureFilename); exitCode != int(cli.ExitSuccess) {
			t.Errorf("exit code = %d, want %d. stderr: %s", exitCode, cli.ExitSuccess, stderr)
		}
	})
}
