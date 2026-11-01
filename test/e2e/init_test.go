//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const preJudgeConfig = `version: "1" # untouched
analysis:
  adr_path: "./docs/arch"
  pipeline:
    pre_judge:
      scorer: jev
      threshold: 0.3
  exclude_patterns:
    - "bin/**"
`

func runInit(t *testing.T, dir, binaryPath, answer string, args ...string) (stdout string, exitCode int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), binaryPath, append([]string{"init"}, args...)...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(answer)

	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return out.String(), exitErr.ExitCode()
	}

	if err != nil {
		t.Fatalf("running init: %v", err)
	}

	return out.String(), 0
}

func TestE2E_InitConfigReplacesARetiredPipeline(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	subDir := filepath.Join(tempDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(subDir, "custom.yaml")
	if err := os.WriteFile(configPath, []byte(preJudgeConfig), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, exitCode := runInit(t, subDir, binaryPath, "y\n", "--config", "custom.yaml")
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Replace(preJudgeConfig, "  pipeline:\n    pre_judge:\n      scorer: jev\n      threshold: 0.3\n",
		"  pipeline:\n    - name: shortlist\n      job: rank\n      with: cosine\n\n    - name: judge\n      job: judge\n      with: llm\n", 1)
	if string(got) != want {
		t.Errorf("config =\n%s\nwant\n%s", got, want)
	}

	if !strings.Contains(stdout, "pre_judge screen was removed") {
		t.Errorf("output should say the screen was removed, got: %s", stdout)
	}

	if !strings.Contains(stdout, "set any of these again") || !strings.Contains(stdout, "      threshold: 0.3") {
		t.Errorf("output should list the removed lines so their settings can be set again, got: %s", stdout)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "archguard.yaml")); !os.IsNotExist(err) {
		t.Errorf("init wrote archguard.yaml at the repo root instead of --config's path, stat err: %v", err)
	}
}

func TestE2E_InitLeavesTheConfigAloneWhenNotConfirmed(t *testing.T) {
	for _, answer := range []string{"n\n", ""} {
		t.Run("answer "+strings.TrimSpace(answer), func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, preJudgeConfig)

			stdout, exitCode := runInit(t, tempDir, binaryPath, answer)
			if exitCode != 0 {
				t.Fatalf("exit code %d, output: %s", exitCode, stdout)
			}

			got, err := os.ReadFile(filepath.Join(tempDir, "archguard.yaml"))
			if err != nil || string(got) != preJudgeConfig {
				t.Errorf("config changed to %q (err %v), want it untouched", got, err)
			}
		})
	}
}

func TestE2E_InitWritesTheDefaultPipelineOnce(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	stdout, exitCode := runInit(t, tempDir, binaryPath, "\ny\nn\n")
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}

	got, err := os.ReadFile(filepath.Join(tempDir, "archguard.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(got), "  pipeline:\n    - name: shortlist\n") {
		t.Errorf("config lacks the default pipeline:\n%s", got)
	}

	stdout, exitCode = runInit(t, tempDir, binaryPath, "")
	if exitCode != 0 || !strings.Contains(stdout, "already has a pipeline list") {
		t.Errorf("second init: exit code %d, output %s; want it to leave the list pipeline alone", exitCode, stdout)
	}
}

func TestE2E_InitConfigCreatesAMissingConfigAtThatPath(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	stdout, exitCode := runInit(t, tempDir, binaryPath, "\nn\n", "--config", "ci.yaml")
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}

	got, err := os.ReadFile(filepath.Join(tempDir, "ci.yaml"))
	if err != nil || !strings.Contains(string(got), "  pipeline:\n    - name: shortlist\n") {
		t.Errorf("ci.yaml = %q (err %v), want a config with the default pipeline", got, err)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "archguard.yaml")); !os.IsNotExist(err) {
		t.Errorf("init wrote archguard.yaml instead of ci.yaml, stat err: %v", err)
	}

	// The path is printed resolved, which can differ from tempDir's spelling on macOS and Windows.
	for _, step := range []string{"archguard index --config ", "archguard check --config "} {
		if !strings.Contains(stdout, step) || !strings.Contains(stdout, string(filepath.Separator)+"ci.yaml\n") {
			t.Errorf("next steps should pass --config with ci.yaml's path, got: %s", stdout)
		}
	}
}
