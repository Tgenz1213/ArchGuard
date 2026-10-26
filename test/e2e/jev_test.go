//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const jevPreJudge = `  pipeline:
    pre_judge:
      scorer: jev
      threshold: 0.3
`

func writeRuledADRs(t *testing.T, dir string) {
	t.Helper()

	adrDir := filepath.Join(dir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}

	rules := map[string]string{"alpha": testutil.MockJevBreaksTrigger + " no logging of secrets", "beta": "no globals"}
	for name, rule := range rules {
		content := fmt.Sprintf("---\ntitle: %q\nstatus: \"Accepted\"\nscope: \"**\"\n---\n\n## Decision\nRule %s.\n\n## Rules\n\n- %s\n", "ADR "+name, name, rule)
		if err := os.WriteFile(filepath.Join(adrDir, "000-"+name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create ADR %s: %v", name, err)
		}
	}
}

func TestE2E_JevPreJudge(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		wantViolation int
		wantKept      int
		wantStderr    string
	}{
		{"only ADRs with a likely-broken rule reach the LLM", violationFixtureContent(), 1, 1, ""},
		{"a Jev failure passes every candidate to the LLM", violationFixtureContent() + "// " + testutil.MockJevFailureTrigger + "\n", 2, 2, "stage pre_judge skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(jevPreJudge))
			writeRuledADRs(t, tempDir)

			if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(tt.fixture), 0644); err != nil {
				t.Fatalf("Failed to create fixture: %v", err)
			}

			stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, []string{config.JevAPIKeyEnv + "=mock_key"}, "--format", "json", fixtureFilename)
			if exitCode != int(cli.ExitDriftDetected) {
				t.Fatalf("expected drift exit code %d, got %d. stderr: %s", cli.ExitDriftDetected, exitCode, stderr)
			}

			var report checkReport
			if err := json.Unmarshal([]byte(stdout), &report); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
			}

			if report.Count != tt.wantViolation {
				t.Errorf("violation count = %d, want %d", report.Count, tt.wantViolation)
			}

			if len(report.Stages) != 2 || report.Stages[1].Name != "pre_judge" || report.Stages[1].Received != 2 || report.Stages[1].Kept != tt.wantKept {
				t.Errorf("stages = %+v, want pre_judge receiving 2 and keeping %d", report.Stages, tt.wantKept)
			}

			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr missing %q, got: %s", tt.wantStderr, stderr)
			}
		})
	}
}

func TestE2E_JevPreJudgeWithoutAPIKeyIsAConfigError(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML(jevPreJudge))
	writeRuledADRs(t, tempDir)

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, []string{config.JevAPIKeyEnv + "="}, "--all")
	if exitCode != int(cli.ExitConfig) {
		t.Fatalf("expected config exit code %d, got %d. stderr: %s", cli.ExitConfig, exitCode, stderr)
	}

	if !strings.Contains(stderr, "analysis.pipeline.pre_judge") || !strings.Contains(stderr, config.JevAPIKeyEnv) {
		t.Errorf("stderr should name the stage and %s, got: %s", config.JevAPIKeyEnv, stderr)
	}
}
