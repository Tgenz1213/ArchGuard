package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func pipelineConfigYAML(pipeline string) string {
	return `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
` + pipeline
}

func writePipelineADRs(t *testing.T, dir string) {
	t.Helper()
	adrDir := filepath.Join(dir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}
	for _, name := range []string{"alpha", "beta"} {
		content := fmt.Sprintf("---\ntitle: %q\nstatus: \"Accepted\"\nscope: \"**\"\n---\n\n## Decision\nRule %s.", "ADR "+name, name)
		if err := os.WriteFile(filepath.Join(adrDir, "000-"+name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create ADR %s: %v", name, err)
		}
	}
}

func TestE2E_PipelineConfig(t *testing.T) {
	tests := []struct {
		name          string
		pipeline      string
		wantViolation int
		wantWarnings  []string
	}{
		{
			name:          "no pipeline judges every candidate",
			pipeline:      "",
			wantViolation: 2,
		},
		{
			name: "rank and rerank narrow the candidates in order",
			pipeline: `  pipeline:
    rank:
      scorer: cosine
      threshold: 0.5
      top_k: 2
    rerank:
      scorer: cosine
      threshold: 0.5
      top_k: 1
`,
			wantViolation: 1,
		},
		{
			name: "rerank alone runs after the default rank and warns for unset keys",
			pipeline: `  pipeline:
    rerank:
      top_k: 1
`,
			wantViolation: 1,
			wantWarnings:  []string{"analysis.pipeline.rerank.threshold not set, defaulting to 0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(tt.pipeline))
			writePipelineADRs(t, tempDir)
			if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
				t.Fatalf("Failed to create fixture: %v", err)
			}

			stdout, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)

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
			for _, want := range tt.wantWarnings {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q, got: %s", want, stderr)
				}
			}
			if strings.Contains(stdout, "Warning") {
				t.Errorf("warnings must stay off stdout, got: %s", stdout)
			}
		})
	}
}

func TestE2E_PipelineOnError_EmbeddingFailure(t *testing.T) {
	tests := []struct {
		name         string
		pipeline     string
		wantExit     cli.ExitCode
		wantFailures int
	}{
		{"no pipeline skips the file", "", cli.ExitSuccess, 0},
		{"on_error skip skips the file", "  pipeline:\n    rank:\n      on_error: skip\n", cli.ExitSuccess, 0},
		{"on_error fail fails the check", "  pipeline:\n    rank:\n      on_error: fail\n", cli.ExitStageUnavailable, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(tt.pipeline))
			writePipelineADRs(t, tempDir)
			fixture := fmt.Sprintf("function f() {\n    console.log(%q);\n}\n", testutil.MockEmbedFailureTrigger)
			if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(fixture), 0644); err != nil {
				t.Fatalf("Failed to create fixture: %v", err)
			}

			stdout, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, fixtureFilename)

			if exitCode != int(tt.wantExit) {
				t.Fatalf("expected exit code %d, got %d. stderr: %s", tt.wantExit, exitCode, stderr)
			}
			var report struct {
				Failures []map[string]string `json:"failures"`
			}
			if err := json.Unmarshal([]byte(stdout), &report); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, stdout)
			}
			if len(report.Failures) != tt.wantFailures {
				t.Fatalf("failures = %v, want %d", report.Failures, tt.wantFailures)
			}
			wantStderr := "generating embedding"
			if tt.wantFailures == 1 {
				wantStderr = "stage rank failed for " + fixtureFilename
			}
			if !strings.Contains(stderr, wantStderr) || !strings.Contains(stderr, "mock embed failure") {
				t.Errorf("stderr should carry %q and the embedding error, got: %s", wantStderr, stderr)
			}
			if tt.wantFailures == 1 {
				f := report.Failures[0]
				if f["stage"] != "rank" || f["kind"] != "unavailable" || f["file"] != fixtureFilename {
					t.Errorf("failure = %v", f)
				}
			}
		})
	}
}

func TestE2E_PipelineOnErrorFail_TakesPrecedenceOverDrift(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML("  pipeline:\n    rank:\n      on_error: fail\n"))
	writePipelineADRs(t, tempDir)
	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}
	failing := "embed-fails.js"
	if err := os.WriteFile(filepath.Join(tempDir, failing), []byte(fmt.Sprintf("console.log(%q);\n", testutil.MockEmbedFailureTrigger)), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	cmd := exec.Command(binaryPath, "check", "--format", "json", fixtureFilename, failing)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != 6 {
		t.Fatalf("expected exit code 6, got err %v. stderr: %s", err, errBuf.String())
	}

	var report struct {
		Count    int                 `json:"count"`
		Failures []map[string]string `json:"failures"`
	}
	if err := json.Unmarshal(outBuf.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, outBuf.String())
	}
	if report.Count == 0 || len(report.Failures) != 1 {
		t.Errorf("count = %d, failures = %v; want the healthy file's drift and one failure both reported", report.Count, report.Failures)
	}
}

func TestE2E_PipelineOnErrorFail_UpdateBaselineDoesNotWriteBaseline(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML("  pipeline:\n    rank:\n      on_error: fail\n"))
	writePipelineADRs(t, tempDir)
	failing := "embed-fails.js"
	if err := os.WriteFile(filepath.Join(tempDir, failing), []byte(fmt.Sprintf("console.log(%q);\n", testutil.MockEmbedFailureTrigger)), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}
	gitAdd(t, tempDir, failing)

	cmd := exec.Command(binaryPath, "check", "--update-baseline")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")
	out, err := cmd.CombinedOutput()
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != 6 {
		t.Fatalf("expected exit code 6, got err %v. Output: %s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(tempDir, baseline.Path)); !os.IsNotExist(statErr) {
		t.Errorf("baseline file must not be written when a stage failed, stat err: %v", statErr)
	}
}

func TestE2E_PipelineConfig_InvalidExitsWithConfigError(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		want     string
	}{
		{"unknown scorer", "  pipeline:\n    rank:\n      scorer: jev\n", "analysis.pipeline.rank.scorer"},
		{"unrecognized stage", "  pipeline:\n    pre_judge:\n      scorer: cosine\n", "analysis.pipeline: unrecognized stage \"pre_judge\""},
		{"out-of-range threshold", "  pipeline:\n    rerank:\n      threshold: 2\n", "analysis.pipeline.rerank.threshold"},
		{"non-positive top_k", "  pipeline:\n    rank:\n      top_k: 0\n", "analysis.pipeline.rank.top_k"},
		{"unknown on_error", "  pipeline:\n    rerank:\n      on_error: warn\n", "analysis.pipeline.rerank.on_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(tt.pipeline))
			writePipelineADRs(t, tempDir)

			stdout, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, "")

			if exitCode != int(cli.ExitConfig) {
				t.Fatalf("expected config exit code %d, got %d. stderr: %s", cli.ExitConfig, exitCode, stderr)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr missing %q, got: %s", tt.want, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout should be empty on config failure, got: %q", stdout)
			}
		})
	}
}
