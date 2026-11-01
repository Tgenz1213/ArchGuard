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

func jevPipeline(onError string) string {
	return pipelineOf(rankNodeYAML("rank", ""), "    - name: breaks-a-rule\n      job: ask\n      with: jev\n      preset: breaks-a-rule\n"+onError)
}

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

func TestE2E_JevAskNode(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		wantViolation int
		wantKept      int
		wantStderr    string
	}{
		{"only ADRs with a likely-broken rule reach the LLM", violationFixtureContent(), 1, 1, ""},
		{"a Jev failure passes every candidate to the LLM", violationFixtureContent() + "// " + testutil.MockJevFailureTrigger + "\n", 2, 2, "node breaks-a-rule skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(jevPipeline("")))
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

			if len(report.Nodes) != 2 || report.Nodes[1].Name != "breaks-a-rule" || report.Nodes[1].Received != 2 || report.Nodes[1].Kept != tt.wantKept {
				t.Errorf("nodes = %+v, want breaks-a-rule receiving 2 and keeping %d", report.Nodes, tt.wantKept)
			}

			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr missing %q, got: %s", tt.wantStderr, stderr)
			}
		})
	}
}

func TestE2E_JevAskNodeWithoutAPIKeyIsAConfigError(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML(jevPipeline("")))
	writeRuledADRs(t, tempDir)

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, []string{config.JevAPIKeyEnv + "="}, "--all")
	if exitCode != int(cli.ExitConfig) {
		t.Fatalf("expected config exit code %d, got %d. stderr: %s", cli.ExitConfig, exitCode, stderr)
	}

	if !strings.Contains(stderr, `node "breaks-a-rule"`) || !strings.Contains(stderr, config.JevAPIKeyEnv) {
		t.Errorf("stderr should name the node and %s, got: %s", config.JevAPIKeyEnv, stderr)
	}
}

func TestE2E_JevRejectedKeyFailsTheCheckAsAConfigError(t *testing.T) {
	for _, onError := range []string{"", "      on_error: skip\n", "      on_error: fail\n"} {
		t.Run("on_error "+strings.TrimSpace(onError), func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(jevPipeline(onError)))
			writeRuledADRs(t, tempDir)

			files := []string{"a.go", "b.go", "c.go"}
			for _, name := range files {
				fixture := violationFixtureContent() + "// " + testutil.MockJevRejectedKeyTrigger + "\n"
				if err := os.WriteFile(filepath.Join(tempDir, name), []byte(fixture), 0644); err != nil {
					t.Fatalf("Failed to create fixture: %v", err)
				}
			}

			_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, []string{config.JevAPIKeyEnv + "=mock_key"}, files...)
			if exitCode != int(cli.ExitConfig) {
				t.Fatalf("expected config exit code %d, got %d. stderr: %s", cli.ExitConfig, exitCode, stderr)
			}

			if got := strings.Count(stderr, config.JevAPIKeyEnv+" was rejected"); got != 1 {
				t.Errorf("stderr names the rejected key %d times, want once: %s", got, stderr)
			}

			if strings.Contains(stderr, "skipped") {
				t.Errorf("stderr should not warn per file, got: %s", stderr)
			}
		})
	}
}

func TestE2E_JevAskNodeReadsBothSidesOfAChange(t *testing.T) {
	const gate = "\tgate() // " + testutil.MockJevStateTrigger + "\n"

	tests := []struct {
		name     string
		before   string
		after    string
		wantKept int
	}{
		{"a removed line is asked about", "package app\n\nfunc a() {\n" + gate + "\troute()\n}\n", "package app\n\nfunc a() {\n\troute()\n}\n", 2},
		{"an added line is asked about", "package app\n\nfunc a() {\n\troute()\n}\n", "package app\n\nfunc a() {\n" + gate + "\troute()\n}\n", 2},
		{"a change that touches neither side's trigger keeps only the broken rule", "package app\n\nfunc a() {\n\troute()\n}\n", "package app\n\nfunc a() {\n\troute()\n\tlog()\n}\n", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, binary := buildE2EBinary(t)
			writeE2EConfig(t, dir, pipelineConfigYAML(jevPipeline("")))
			writeRuledADRs(t, dir)
			commitFile(t, dir, "app.go", tt.before, "base")
			commitFile(t, dir, "app.go", tt.after, "change")

			stdout, stderr, _ := runCheckWithEnv(t, dir, binary, []string{config.JevAPIKeyEnv + "=mock_key"}, "--format", "json", "--since", "HEAD~1")

			var report checkReport
			if err := json.Unmarshal([]byte(stdout), &report); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %q\nstderr: %s", err, stdout, stderr)
			}

			if len(report.Nodes) != 2 || report.Nodes[1].Name != "breaks-a-rule" || report.Nodes[1].Received != 2 || report.Nodes[1].Kept != tt.wantKept {
				t.Errorf("nodes = %+v, want breaks-a-rule receiving 2 and keeping %d", report.Nodes, tt.wantKept)
			}
		})
	}
}
