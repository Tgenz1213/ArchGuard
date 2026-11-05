//go:build e2e

package e2e

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const profilesConfigYAML = `
version: "1"
llm:
  provider: "ollama"
  cost_per_1k_input_tokens: 0.5
  cost_per_1k_output_tokens: 2
llms:
  strong:
    provider: "claude"
    model: "big"
    max_tokens: 150000
    cost_per_1k_input_tokens: 3
    cost_per_1k_output_tokens: 15
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`

func setupProfilesRepo(t *testing.T, pipeline string) (dir, binaryPath string) {
	t.Helper()

	dir, binaryPath = buildE2EBinary(t)

	writeE2EConfig(t, dir, profilesConfigYAML+pipeline)
	writeNoSecretsADR(t, dir)

	if err := os.WriteFile(filepath.Join(dir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, dir, binaryPath, int(cli.ExitSuccess))

	return dir, binaryPath
}

func TestE2E_JudgeProfileAndSuggestNode(t *testing.T) {
	pipeline := "  pipeline:\n" + shortlistNodeYAML + judgeNodeYAML + "      llm: strong\n    - name: suggest\n      job: suggest\n      with: llm\n"
	dir, binaryPath := setupProfilesRepo(t, pipeline)

	stdout, stderr, exitCode := runCheckWithEnv(t, dir, binaryPath, nil, "--format", "json", "--suggest-fixes", fixtureFilename)
	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code = %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
	}

	var report checkReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
	}

	if len(report.Violations) != 1 || report.Violations[0].Suggestion == "" {
		t.Fatalf("violations = %+v, want one with a suggestion", report.Violations)
	}

	var usage usageReport
	if err := json.Unmarshal([]byte(stdout), &usage); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}

	assertPricedCall(t, "judgment", &usage.Usage.Judgment, 3, 15)
	assertPricedCall(t, "suggestion", usage.Usage.Suggestion, 0.5, 2)
}

func assertPricedCall(t *testing.T, role string, got *chatUsage, inputPrice, outputPrice float64) {
	t.Helper()

	if got == nil || got.Calls != 1 || got.Cost == nil {
		t.Fatalf("%s usage = %+v, want one priced call", role, got)
	}

	want := float64(testutil.MockChatInputTokens)/1000*inputPrice + float64(testutil.MockChatOutputTokens)/1000*outputPrice
	if math.Abs(*got.Cost-want) > 1e-9 {
		t.Errorf("%s cost = %v, want %v from its profile's prices", role, *got.Cost, want)
	}
}

func TestE2E_UnknownProfileExitsConfig(t *testing.T) {
	dir, binaryPath := setupProfilesRepo(t, "  pipeline:\n"+shortlistNodeYAML+judgeNodeYAML+"      llm: weak\n")

	_, stderr, exitCode := runCheckWithEnv(t, dir, binaryPath, nil, fixtureFilename)
	if exitCode != int(cli.ExitConfig) {
		t.Fatalf("exit code = %d, want %d. stderr: %s", exitCode, cli.ExitConfig, stderr)
	}

	if !strings.Contains(stderr, `node "judge".llm`) || !strings.Contains(stderr, `"weak"`) {
		t.Errorf("stderr = %q, want the node and the profile named", stderr)
	}
}
