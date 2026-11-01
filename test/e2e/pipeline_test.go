//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
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

const judgeNodeYAML = "    - name: judge\n      job: judge\n      with: llm\n"

func rankNodeYAML(name, settings string) string {
	return "    - name: " + name + "\n      job: rank\n      with: cosine\n" + settings
}

// defaultPipelineYAML is the pipeline archguard init writes, as e2e configs put it under analysis.
const defaultPipelineYAML = "  pipeline:\n" + shortlistNodeYAML + judgeNodeYAML

const shortlistNodeYAML = "    - name: shortlist\n      job: rank\n      with: cosine\n"

// pipelineOf ends the given nodes with the judge node.
func pipelineOf(nodes ...string) string {
	return "  pipeline:\n" + strings.Join(nodes, "") + judgeNodeYAML
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

type nodeTotals struct {
	name           string
	received, kept int
}

func TestE2E_PipelineConfig(t *testing.T) {
	tests := []struct {
		name          string
		pipeline      string
		wantViolation int
		wantNodes     []nodeTotals
	}{
		{
			name:          "the default pipeline judges every candidate",
			pipeline:      defaultPipelineYAML,
			wantViolation: 2,
			wantNodes:     []nodeTotals{{"shortlist", 2, 2}},
		},
		{
			name: "two rank nodes narrow the candidates in order",
			pipeline: pipelineOf(
				rankNodeYAML("rank", "      threshold: 0.5\n      top_k: 2\n"),
				rankNodeYAML("rerank", "      threshold: 0.5\n      top_k: 1\n"),
			),
			wantViolation: 1,
			wantNodes:     []nodeTotals{{"rank", 2, 2}, {"rerank", 2, 1}},
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

			if len(report.Nodes) != len(tt.wantNodes) {
				t.Fatalf("nodes = %+v, want %d entries", report.Nodes, len(tt.wantNodes))
			}

			for i, want := range tt.wantNodes {
				got := report.Nodes[i]
				if got.Name != want.name || got.Received != want.received || got.Kept != want.kept || got.DurationMS == nil {
					t.Errorf("node %d = %+v, want %+v with a duration_ms", i, got, want)
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
		{"unset on_error skips the file, leaving nothing checked", pipelineOf(rankNodeYAML("rank", "")), cli.ExitNothingChecked, 0},
		{"on_error skip skips the file, leaving nothing checked", pipelineOf(rankNodeYAML("rank", "      on_error: skip\n")), cli.ExitNothingChecked, 0},
		{"on_error fail fails the check", pipelineOf(rankNodeYAML("rank", "      on_error: fail\n")), cli.ExitNodeUnavailable, 1},
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
				wantStderr = fixtureFilename + "\n  Error: node rank failed"
			}

			if !strings.Contains(stderr, wantStderr) || !strings.Contains(stderr, "mock embed failure") {
				t.Errorf("stderr should carry %q and the embedding error, got: %s", wantStderr, stderr)
			}

			if tt.wantFailures == 1 {
				f := report.Failures[0]
				if f["node"] != "rank" || f["kind"] != "unavailable" || f["file"] != fixtureFilename {
					t.Errorf("failure = %v", f)
				}
			}
		})
	}
}

func TestE2E_PipelineOnErrorFail_TakesPrecedenceOverDrift(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML(pipelineOf(rankNodeYAML("rank", "      on_error: fail\n"))))
	writePipelineADRs(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	failing := "embed-fails.js"
	if err := os.WriteFile(filepath.Join(tempDir, failing), []byte(fmt.Sprintf("console.log(%q);\n", testutil.MockEmbedFailureTrigger)), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), binaryPath, "check", "--format", "json", fixtureFilename, failing)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()

	var exitError *exec.ExitError
	ok := errors.As(err, &exitError)

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
	writeE2EConfig(t, tempDir, pipelineConfigYAML(pipelineOf(rankNodeYAML("rank", "      on_error: fail\n"))))
	writePipelineADRs(t, tempDir)

	failing := "embed-fails.js"
	if err := os.WriteFile(filepath.Join(tempDir, failing), []byte(fmt.Sprintf("console.log(%q);\n", testutil.MockEmbedFailureTrigger)), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	gitAdd(t, tempDir, failing)

	cmd := exec.CommandContext(t.Context(), binaryPath, "check", "--update-baseline")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "ARCHGUARD_API_KEY=mock_key")
	out, err := cmd.CombinedOutput()

	var exitError *exec.ExitError
	ok := errors.As(err, &exitError)

	if !ok || exitError.ExitCode() != 6 {
		t.Fatalf("expected exit code 6, got err %v. Output: %s", err, out)
	}

	if _, statErr := os.Stat(filepath.Join(tempDir, baseline.Path)); !os.IsNotExist(statErr) {
		t.Errorf("baseline file must not be written when a node failed, stat err: %v", statErr)
	}
}

func TestE2E_PipelineConfig_InvalidExitsWithConfigError(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		want     string
	}{
		{"missing pipeline", "", "archguard init"},
		{"retired mapping", "  pipeline:\n    rank:\n      top_k: 2\n", "archguard init"},
		{"unknown runner", pipelineOf("    - name: rank\n      job: rank\n      with: bm25\n"), `node "rank".with`},
		{"no judge", "  pipeline:\n" + rankNodeYAML("rank", ""), "exactly one judge"},
		{"ask first", "  pipeline:\n    - name: screen\n      job: ask\n      with: jev\n      preset: breaks-a-rule\n" + judgeNodeYAML, `"screen", must be a rank node`},
		{"out-of-range threshold", pipelineOf(rankNodeYAML("rank", "      threshold: 2\n")), `node "rank".threshold`},
		{"non-positive top_k", pipelineOf(rankNodeYAML("rank", "      top_k: 0\n")), `node "rank".top_k`},
		{"unknown on_error", pipelineOf(rankNodeYAML("rank", "      on_error: warn\n")), `node "rank".on_error`},
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

func TestE2E_IndexRunsOnAPipelineCheckRejects(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML("  pipeline:\n    rank:\n      top_k: 2\n"))
	writePipelineADRs(t, tempDir)

	runIndexCmd(t, tempDir, binaryPath, 0)
}

func TestE2E_PipelineIsCheckedBeforeTheVectorStoreConnects(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	unreachable := strings.Replace(pipelineConfigYAML(""), "  embedding_dim: 768\n", "  embedding_dim: 768\n  connection_string: \"postgres://archguard@127.0.0.1:1/none\"\n", 1)
	writeE2EConfig(t, tempDir, unreachable)
	writePipelineADRs(t, tempDir)

	_, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, "")
	if exitCode != int(cli.ExitConfig) || !strings.Contains(stderr, "archguard init") {
		t.Fatalf("expected config exit code %d naming archguard init before any database connection, got %d. stderr: %s", cli.ExitConfig, exitCode, stderr)
	}
}

func TestE2E_MissingPipelineNamesTheConfigToInit(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writePipelineADRs(t, tempDir)

	configPath := filepath.Join(tempDir, "ci.yaml")
	if err := os.WriteFile(configPath, []byte(pipelineConfigYAML("")), 0644); err != nil {
		t.Fatal(err)
	}

	// The path is printed resolved, which can differ from tempDir's spelling on macOS and Windows.
	stderr, exitCode := runArchguard(t, tempDir, binaryPath, "check", "--config", "ci.yaml", "--all")
	if exitCode != int(cli.ExitConfig) || !strings.Contains(stderr, "archguard init --config ") || !strings.Contains(stderr, "ci.yaml`") {
		t.Errorf("exit code %d, stderr %s; want 3 and the init command for ci.yaml", exitCode, stderr)
	}
}
