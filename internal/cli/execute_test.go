package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const (
	executeTestADR = "---\ntitle: \"No println\"\nstatus: \"Accepted\"\nscope: \"**\"\n---\n\n## Decision\nCode must not call println.\n"

	cleanSource    = "package main\n\nfunc main() {}\n"
	printlnCall    = `println("hi")`
	violatingCode  = "package main\n\nfunc main() {\n\t" + printlnCall + "\n}\n"
	embedFailsCode = "package main\n\n// " + testutil.MockEmbedFailureTrigger + "\n"

	defaultPipelineNodes = "  pipeline:\n    - name: shortlist\n      job: rank\n      with: cosine\n    - name: judge\n      job: judge\n      with: llm\n"
	failingRankNodes     = "  pipeline:\n    - name: shortlist\n      job: rank\n      with: cosine\n      on_error: fail\n    - name: judge\n      job: judge\n      with: llm\n"
	noJudgeNodes         = "  pipeline:\n    - name: shortlist\n      job: rank\n      with: cosine\n"
)

func executeTestConfig(pipeline string) string {
	return "version: \"1\"\nllm:\n  provider: \"ollama\"\nvector_store:\n  provider: \"ollama\"\n  embedding_dim: 8\nanalysis:\n  adr_path: \"./docs/arch\"\n  accepted_statuses: [\"Accepted\"]\n" + pipeline
}

func setupCheckRepo(t *testing.T, pipeline, source string) {
	t.Helper()

	setupExecuteTestRepo(t)
	writeTestFile(t, configFilename, executeTestConfig(pipeline))
	writeTestFile(t, filepath.Join("docs", "arch", "0001-no-println.md"), executeTestADR)
	writeTestFile(t, "main.go", source)

	gitAdd := exec.CommandContext(t.Context(), "git", "add", "main.go")
	if out, err := gitAdd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type judgeModel struct {
	calls atomic.Int32
}

func (j *judgeModel) factories() ProviderFactories {
	return ProviderFactories{Chat: func(cfg *config.Config) inference.Provider {
		return &inference.MockProvider{
			EmbeddingDim: cfg.VectorStore.EmbeddingDim,
			ChatFunc:     j.chat,
			EmbedFunc:    embedUnlessTriggered(cfg.VectorStore.EmbeddingDim),
		}
	}}
}

func (j *judgeModel) chat(_ context.Context, _, user string) (string, error) {
	j.calls.Add(1)

	result := inference.AnalysisResult{Reasoning: "no println"}
	if strings.Contains(user, printlnCall) {
		result = inference.AnalysisResult{Violation: true, Reasoning: "calls println", QuotedCode: printlnCall}
	}

	reply, err := json.Marshal(result)

	return string(reply), err
}

func embedUnlessTriggered(dim int) func(context.Context, string, inference.EmbeddingTaskType) ([]float32, error) {
	return func(_ context.Context, text string, _ inference.EmbeddingTaskType) ([]float32, error) {
		if strings.Contains(text, testutil.MockEmbedFailureTrigger) {
			return nil, errors.New("embedding service down")
		}

		vector := make([]float32, dim)
		vector[0] = 1

		return vector, nil
	}
}

// runExecute logs the run's error, so a wrong exit code shows why. The environment would otherwise send a run to the developer's database or, in CI, down the annotation path that a local run never takes, so the coverage total would differ between the two.
func runExecute(t *testing.T, factories ProviderFactories, args ...string) (ExitCode, string) {
	t.Helper()

	t.Setenv("ARCHGUARD_DB_URL", "")
	t.Setenv("GITHUB_ACTIONS", "")

	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = append([]string{"archguard"}, args...)

	var (
		code ExitCode
		err  error
	)

	stdout := captureStdout(t, func() {
		captureStderr(t, func() {
			code, err = Execute(t.Context(), "test", factories)
		})
	})

	if err != nil {
		t.Logf("archguard %s: %v", strings.Join(args, " "), err)
	}

	return code, stdout
}

func TestExecute_CheckExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		source   string
		want     ExitCode
	}{
		{"clean file passes", defaultPipelineNodes, cleanSource, ExitSuccess},
		{"violation is drift", defaultPipelineNodes, violatingCode, ExitDriftDetected},
		{"pipeline without a judge is a config error", noJudgeNodes, cleanSource, ExitConfig},
		{"failed node under on_error fail", failingRankNodes, embedFailsCode, ExitNodeUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupCheckRepo(t, tt.pipeline, tt.source)

			judge := &judgeModel{}
			if code, _ := runExecute(t, judge.factories(), "check", "main.go"); code != tt.want {
				t.Errorf("check exit = %d, want %d", code, tt.want)
			}
		})
	}
}

func TestExecute_UpdateBaselineAcceptsExistingDrift(t *testing.T) {
	setupCheckRepo(t, defaultPipelineNodes, violatingCode)

	judge := &judgeModel{}
	if code, _ := runExecute(t, judge.factories(), "check", "--update-baseline", "--baseline-reason", "legacy"); code != ExitSuccess {
		t.Fatalf("check --update-baseline exit = %d, want %d", code, ExitSuccess)
	}

	saved, err := baseline.Load(baseline.Path)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}

	if len(saved.Entries) != 1 || saved.Entries[0].Reason != "legacy" {
		t.Errorf("baseline entries = %+v, want one entry with reason legacy", saved.Entries)
	}

	if code, _ := runExecute(t, judge.factories(), "check", "main.go"); code != ExitSuccess {
		t.Errorf("check after baselining exit = %d, want %d", code, ExitSuccess)
	}
}

func TestExecute_UpdateBaselineWithFailedNodeWritesNoBaseline(t *testing.T) {
	setupCheckRepo(t, failingRankNodes, embedFailsCode)

	judge := &judgeModel{}
	if code, _ := runExecute(t, judge.factories(), "check", "--update-baseline"); code != ExitNodeUnavailable {
		t.Errorf("check --update-baseline exit = %d, want %d", code, ExitNodeUnavailable)
	}

	if _, err := os.Stat(baseline.Path); !os.IsNotExist(err) {
		t.Errorf("baseline file exists after a failed node (stat err %v)", err)
	}
}

func TestExecute_DryRunJudgesNothing(t *testing.T) {
	setupCheckRepo(t, defaultPipelineNodes, violatingCode)

	judge := &judgeModel{}

	code, stdout := runExecute(t, judge.factories(), "check", "--dry-run", "--format", "json", "main.go")
	if code != ExitSuccess {
		t.Fatalf("check --dry-run exit = %d, want %d", code, ExitSuccess)
	}

	if calls := judge.calls.Load(); calls != 0 {
		t.Errorf("dry run made %d judgment call(s), want 0", calls)
	}

	var report checkReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not a JSON report: %v\n%s", err, stdout)
	}

	if report.DryRun == nil || report.DryRun.Calls != 1 {
		t.Errorf("dry_run = %+v, want 1 projected judgment call", report.DryRun)
	}
}

func TestExecute_OutputWritesTheReportFile(t *testing.T) {
	setupCheckRepo(t, defaultPipelineNodes, violatingCode)

	judge := &judgeModel{}

	code, stdout := runExecute(t, judge.factories(), "check", "--format", "json", "--output", "report.json", "main.go")
	if code != ExitDriftDetected {
		t.Fatalf("check exit = %d, want %d", code, ExitDriftDetected)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want the report only in the file", stdout)
	}

	content, err := os.ReadFile("report.json")
	if err != nil {
		t.Fatalf("report file not written: %v", err)
	}

	var report checkReport
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatalf("report file is not JSON: %v", err)
	}

	if report.Count != 1 {
		t.Errorf("report count = %d, want 1", report.Count)
	}
}

func TestExecute_OutputRefusesTheBaselineFile(t *testing.T) {
	setupCheckRepo(t, defaultPipelineNodes, cleanSource)
	writeTestFile(t, baseline.Path, "{}\n")

	judge := &judgeModel{}
	if code, _ := runExecute(t, judge.factories(), "check", "--output", baseline.Path, "main.go"); code != ExitError {
		t.Errorf("check exit = %d, want %d", code, ExitError)
	}

	if content, err := os.ReadFile(baseline.Path); err != nil || string(content) != "{}\n" {
		t.Errorf("baseline file was changed: %q, %v", content, err)
	}

	if calls := judge.calls.Load(); calls != 0 {
		t.Errorf("check judged %d time(s) before refusing the destination, want 0", calls)
	}
}

func TestExecute_Index(t *testing.T) {
	setupCheckRepo(t, defaultPipelineNodes, cleanSource)

	judge := &judgeModel{}
	if code, _ := runExecute(t, judge.factories(), "index"); code != ExitSuccess {
		t.Fatalf("index exit = %d, want %d", code, ExitSuccess)
	}

	content, err := os.ReadFile(filepath.Join(".archguard", "index.json"))
	if err != nil {
		t.Fatalf("index not written: %v", err)
	}

	if !strings.Contains(string(content), "0001-no-println.md") {
		t.Errorf("index does not hold the ADR:\n%s", content)
	}
}

func TestExecute_IndexWithNoValidADRFails(t *testing.T) {
	setupCheckRepo(t, defaultPipelineNodes, cleanSource)
	writeTestFile(t, filepath.Join("docs", "arch", "0001-no-println.md"), "no frontmatter\n")

	judge := &judgeModel{}
	if code, _ := runExecute(t, judge.factories(), "index"); code != ExitIndexError {
		t.Errorf("index exit = %d, want %d", code, ExitIndexError)
	}

	if _, err := os.Stat(filepath.Join(".archguard", "index.json")); !os.IsNotExist(err) {
		t.Errorf("index written with no valid ADR (stat err %v)", err)
	}
}
