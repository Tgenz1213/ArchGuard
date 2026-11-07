//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const governsNodeYAML = "    - name: governs\n      job: ask\n      with: jev\n      preset: governs\n"

func runGoverns(t *testing.T, pipeline, fixture string) (stdout, stderr string, exitCode int) {
	t.Helper()

	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML(pipeline))
	writeRuledADRs(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(fixture), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	return runCheckWithEnv(t, tempDir, binaryPath, []string{config.JevAPIKeyEnv + "=mock_key"}, "--format", "json", fixtureFilename)
}

func TestE2E_GovernsAsTheFirstNode(t *testing.T) {
	stdout, stderr, exitCode := runGoverns(t, "  pipeline:\n"+governsNodeYAML+judgeNodeYAML, violationFixtureContent())
	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code = %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
	}

	var report checkReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
	}

	if len(report.Nodes) != 1 || report.Nodes[0].Name != "governs" || report.Nodes[0].Received != 2 || report.Nodes[0].Kept != 1 {
		t.Errorf("nodes = %+v, want governs receiving both scoped ADRs and keeping the one it governs", report.Nodes)
	}

	if report.Count != 1 || report.Violations[0].ADRTitle != "ADR alpha" {
		t.Errorf("violations = %+v, want only ADR alpha judged", report.Violations)
	}
}

func TestE2E_GovernsFailureOnTheFirstNodeFailsByDefault(t *testing.T) {
	_, stderr, exitCode := runGoverns(t, "  pipeline:\n"+governsNodeYAML+judgeNodeYAML, violationFixtureContent()+"// "+testutil.MockJevFailureTrigger+"\n")
	if exitCode != int(cli.ExitNodeUnavailable) {
		t.Fatalf("exit code = %d, want %d: a first ask node can't pass every ADR on. stderr: %s", exitCode, cli.ExitNodeUnavailable, stderr)
	}
}

func TestE2E_GovernsRejectedKeyIsAConfigError(t *testing.T) {
	pipelines := map[string]string{
		"first node, fail by default": "  pipeline:\n" + governsNodeYAML + judgeNodeYAML,
		"after the shortlist, skip":   pipelineOf(shortlistNodeYAML, governsNodeYAML+"      on_error: skip\n"),
	}

	for name, pipeline := range pipelines {
		t.Run(name, func(t *testing.T) {
			_, stderr, exitCode := runGoverns(t, pipeline, violationFixtureContent()+"// "+testutil.MockJevRejectedKeyTrigger+"\n")
			if exitCode != int(cli.ExitConfig) {
				t.Fatalf("exit code = %d, want %d. stderr: %s", exitCode, cli.ExitConfig, stderr)
			}

			if !strings.Contains(stderr, config.JevAPIKeyEnv+" was rejected") {
				t.Errorf("stderr = %q, want the rejected key named", stderr)
			}
		})
	}
}
