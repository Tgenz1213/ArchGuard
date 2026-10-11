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
}

type usageReport struct {
	Usage struct {
		Judgment   chatUsage  `json:"judgment"`
		Suggestion *chatUsage `json:"suggestion"`
		Embedding  struct {
			Tokens *int     `json:"tokens"`
			Cost   *float64 `json:"estimated_cost"`
		} `json:"embedding"`
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
`)
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

func TestE2E_CheckReportsTokenUsage(t *testing.T) {
	dir, binaryPath := setupUsageRepo(t)

	t.Run("first run pays for one judgment, one suggestion and one query embedding", func(t *testing.T) {
		report := runUsageCheck(t, dir, binaryPath)

		assertChatTokens(t, "judgment", &report.Usage.Judgment, 1)
		assertChatTokens(t, "suggestion", report.Usage.Suggestion, 1)

		embedding := report.Usage.Embedding
		if embedding.Tokens == nil || *embedding.Tokens != testutil.MockEmbedTokens {
			t.Errorf("embedding tokens = %v, want %d", embedding.Tokens, testutil.MockEmbedTokens)
		}
	})

	t.Run("cache hits add no chat tokens", func(t *testing.T) {
		report := runUsageCheck(t, dir, binaryPath)

		assertChatTokens(t, "judgment", &report.Usage.Judgment, 0)
		assertChatTokens(t, "suggestion", report.Usage.Suggestion, 0)
	})
}
