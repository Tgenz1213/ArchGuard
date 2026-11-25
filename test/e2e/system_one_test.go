//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/config"
)

const (
	// The mock System One server answers yes to a rule naming mockAskBreaksTrigger and fails on a state holding mockAskFailureTrigger.
	mockAskBreaksTrigger  = "TRIGGER_ASK_BREAKS"
	mockAskFailureTrigger = "TRIGGER_ASK_FAILURE"
	// mockAskStateTrigger makes the mock answer yes to every question about a side of the code holding it.
	mockAskStateTrigger = "TRIGGER_ASK_STATE"
	// mockAskRejectedKeyTrigger makes the mock answer as a server does to a rejected API key (401).
	mockAskRejectedKeyTrigger = "TRIGGER_ASK_REJECTED_KEY"

	mockAskKey          = "mock_system_one_key"
	mockAskInputTokens  = 40
	mockAskOutputTokens = 2
)

type mockSystemOne struct {
	URL     string
	sawKey  atomic.Bool
	sawNone atomic.Bool
}

func startMockSystemOne(t *testing.T) *mockSystemOne {
	t.Helper()

	mock := &mockSystemOne{}
	server := httptest.NewServer(http.HandlerFunc(mock.answer))
	t.Cleanup(server.Close)
	mock.URL = server.URL

	return mock
}

func (m *mockSystemOne) answer(w http.ResponseWriter, r *http.Request) {
	switch r.Header.Get("Authorization") {
	case "":
		m.sawNone.Store(true)
	case "Bearer " + mockAskKey:
		m.sawKey.Store(true)
	}

	var request struct {
		State     string `json:"state"`
		Questions map[string]struct {
			Instructions string `json:"instructions"`
		} `json:"questions"`
	}
	if r.URL.Path != "/v1/systemone" || json.NewDecoder(r.Body).Decode(&request) != nil {
		http.Error(w, `{"detail":"bad request"}`, http.StatusUnprocessableEntity)
		return
	}

	if strings.Contains(request.State, mockAskRejectedKeyTrigger) {
		http.Error(w, `{"detail":{"error_type":"authentication_error"}}`, http.StatusUnauthorized)
		return
	}

	if strings.Contains(request.State, mockAskFailureTrigger) {
		http.Error(w, `{"detail":"mock failure (E2E trigger)"}`, http.StatusInternalServerError)
		return
	}

	answers := map[string]map[string]any{}
	for name, question := range request.Questions {
		yes := 0.05
		if strings.Contains(question.Instructions, mockAskBreaksTrigger) || strings.Contains(request.State, mockAskStateTrigger) {
			yes = 0.95
		}

		answers[name] = map[string]any{"type": "noul", "noul": yes}
	}

	body := map[string]any{"model": "jev-latest", "answers": answers, "usage": map[string]int{"input_tokens": mockAskInputTokens, "output_tokens": mockAskOutputTokens}}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (m *mockSystemOne) configYAML(pipeline string) string {
	return pipelineConfigYAML(pipeline) + "system_one:\n  base_url: " + m.URL + "\n"
}

func (m *mockSystemOne) env() []string {
	return []string{config.SystemOneAPIKeyEnv + "=" + mockAskKey, config.SystemOneKeyOriginEnv + "=" + m.URL}
}

func askPipeline(onError string) string {
	return pipelineOf(rankNodeYAML("rank", ""), "    - name: breaks-a-rule\n      job: ask\n      with: system-one\n      preset: breaks-a-rule\n"+onError)
}

func writeRuledADRs(t *testing.T, dir string) {
	t.Helper()

	adrDir := filepath.Join(dir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}

	rules := map[string]string{"alpha": mockAskBreaksTrigger + " no logging of secrets", "beta": "no globals"}
	for name, rule := range rules {
		content := fmt.Sprintf("---\ntitle: %q\nstatus: \"Accepted\"\nscope: \"**\"\n---\n\n## Decision\nRule %s.\n\n## Rules\n\n- %s\n", "ADR "+name, name, rule)
		if err := os.WriteFile(filepath.Join(adrDir, "000-"+name+".md"), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create ADR %s: %v", name, err)
		}
	}
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}
}

func decodeCheckReport(t *testing.T, stdout, stderr string) checkReport {
	t.Helper()

	var report checkReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q\nstderr: %s", err, stdout, stderr)
	}

	return report
}

func TestE2E_SystemOneAskNode(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		wantViolation int
		wantKept      int
		wantStderr    string
	}{
		{"only ADRs with a likely-broken rule reach the LLM", violationFixtureContent(), 1, 1, ""},
		{"a failed request passes every candidate to the LLM", violationFixtureContent() + "// " + mockAskFailureTrigger + "\n", 2, 2, "node breaks-a-rule skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := startMockSystemOne(t)
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
			writeRuledADRs(t, tempDir)
			writeFixture(t, tempDir, fixtureFilename, tt.fixture)

			stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, mock.env(), "--format", "json", fixtureFilename)
			if exitCode != int(cli.ExitDriftDetected) {
				t.Fatalf("expected drift exit code %d, got %d. stderr: %s", cli.ExitDriftDetected, exitCode, stderr)
			}

			report := decodeCheckReport(t, stdout, stderr)

			if report.Count != tt.wantViolation {
				t.Errorf("violation count = %d, want %d", report.Count, tt.wantViolation)
			}

			if len(report.Nodes) != 2 || report.Nodes[1].Name != "breaks-a-rule" || report.Nodes[1].Received != 2 || report.Nodes[1].Kept != tt.wantKept {
				t.Errorf("nodes = %+v, want breaks-a-rule receiving 2 and keeping %d", report.Nodes, tt.wantKept)
			}

			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr missing %q, got: %s", tt.wantStderr, stderr)
			}

			if !mock.sawKey.Load() || mock.sawNone.Load() {
				t.Errorf("the mock saw the key: %v, a request without it: %v; want the key on every request to its origin", mock.sawKey.Load(), mock.sawNone.Load())
			}
		})
	}
}

func TestE2E_SystemOneRefusedConnectionFollowsOnError(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	tests := []struct {
		name     string
		onError  string
		wantExit cli.ExitCode
	}{
		{"skip passes every candidate on", "", cli.ExitDriftDetected},
		{"fail stops the check", "      on_error: fail\n", cli.ExitNodeUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, pipelineConfigYAML(askPipeline(tt.onError))+"system_one:\n  base_url: "+closed.URL+"\n")
			writeRuledADRs(t, tempDir)
			writeFixture(t, tempDir, fixtureFilename, violationFixtureContent())

			env := []string{config.SystemOneAPIKeyEnv + "=", config.SystemOneKeyOriginEnv + "="}

			_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, "--format", "json", fixtureFilename)
			if exitCode != int(tt.wantExit) {
				t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, tt.wantExit, stderr)
			}
		})
	}
}

func TestE2E_SystemOneAskNodeRunsWithoutAKey(t *testing.T) {
	mock := startMockSystemOne(t)
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
	writeRuledADRs(t, tempDir)
	writeFixture(t, tempDir, fixtureFilename, violationFixtureContent())

	env := []string{config.SystemOneAPIKeyEnv + "=", config.SystemOneKeyOriginEnv + "="}

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, "--format", "json", fixtureFilename)
	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
	}

	if mock.sawKey.Load() || !mock.sawNone.Load() {
		t.Errorf("the mock saw a key: %v; want every request without one", mock.sawKey.Load())
	}
}

func TestE2E_SystemOneKeyGoesOnlyToItsOrigin(t *testing.T) {
	mock := startMockSystemOne(t)
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
	writeRuledADRs(t, tempDir)
	writeFixture(t, tempDir, fixtureFilename, violationFixtureContent())

	env := []string{config.SystemOneAPIKeyEnv + "=" + mockAskKey, config.SystemOneKeyOriginEnv + "="}

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, "--format", "json", fixtureFilename)
	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
	}

	if mock.sawKey.Load() {
		t.Error("the key reached a server whose origin isn't the key's")
	}
}

func TestE2E_SystemOneKeyOriginInDotEnvIsIgnored(t *testing.T) {
	mock := startMockSystemOne(t)
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
	writeRuledADRs(t, tempDir)
	writeFixture(t, tempDir, fixtureFilename, violationFixtureContent())
	writeFixture(t, tempDir, ".env", config.SystemOneKeyOriginEnv+"="+mock.URL+"\n")

	t.Setenv(config.SystemOneKeyOriginEnv, "")

	if err := os.Unsetenv(config.SystemOneKeyOriginEnv); err != nil {
		t.Fatal(err)
	}

	env := []string{config.SystemOneAPIKeyEnv + "=" + mockAskKey}

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, "--format", "json", fixtureFilename)
	if exitCode != int(cli.ExitDriftDetected) {
		t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitDriftDetected, stderr)
	}

	if mock.sawKey.Load() {
		t.Error("a .env in the repository sent the key to the server it named")
	}
}

func TestE2E_SystemOneHostedJevWithoutAKeyIsAConfigError(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, pipelineConfigYAML(askPipeline("")))
	writeRuledADRs(t, tempDir)

	env := []string{config.SystemOneAPIKeyEnv + "=", config.SystemOneKeyOriginEnv + "="}

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, "--all")
	if exitCode != int(cli.ExitConfig) {
		t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitConfig, stderr)
	}

	if !strings.Contains(stderr, `node "breaks-a-rule"`) || !strings.Contains(stderr, config.SystemOneAPIKeyEnv) {
		t.Errorf("stderr should name the node and %s, got: %s", config.SystemOneAPIKeyEnv, stderr)
	}
}

func TestE2E_SystemOneKeyOverPlainHTTPToAnotherMachineIsAConfigError(t *testing.T) {
	mock := startMockSystemOne(t)
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
	writeRuledADRs(t, tempDir)

	env := []string{config.SystemOneAPIKeyEnv + "=" + mockAskKey, config.SystemOneKeyOriginEnv + "=http://example.com"}

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, "--all")
	if exitCode != int(cli.ExitConfig) {
		t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitConfig, stderr)
	}

	if !strings.Contains(stderr, "plain http") {
		t.Errorf("stderr should say the key would go over plain http, got: %s", stderr)
	}
}

func TestE2E_SystemOneRejectedKeyFailsTheCheckAsAConfigError(t *testing.T) {
	for _, onError := range []string{"", "      on_error: skip\n", "      on_error: fail\n"} {
		t.Run("on_error "+strings.TrimSpace(onError), func(t *testing.T) {
			mock := startMockSystemOne(t)
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, mock.configYAML(askPipeline(onError)))
			writeRuledADRs(t, tempDir)

			files := []string{"a.go", "b.go", "c.go"}
			for _, name := range files {
				writeFixture(t, tempDir, name, violationFixtureContent()+"// "+mockAskRejectedKeyTrigger+"\n")
			}

			_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, mock.env(), files...)
			if exitCode != int(cli.ExitConfig) {
				t.Fatalf("expected config exit code %d, got %d. stderr: %s", cli.ExitConfig, exitCode, stderr)
			}

			want := config.SystemOneAPIKeyEnv + " was rejected by " + mock.URL + " (node breaks-a-rule)"
			if got := strings.Count(stderr, want); got != 1 {
				t.Errorf("stderr has %q %d times, want once: %s", want, got, stderr)
			}

			if strings.Contains(stderr, "skipped") {
				t.Errorf("stderr should not warn per file, got: %s", stderr)
			}
		})
	}
}

func TestE2E_SystemOneServerThatNeedsAKeyItNeverGets(t *testing.T) {
	mock := startMockSystemOne(t)
	tempDir, binaryPath := buildE2EBinary(t)
	writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
	writeRuledADRs(t, tempDir)
	writeFixture(t, tempDir, fixtureFilename, violationFixtureContent()+"// "+mockAskRejectedKeyTrigger+"\n")

	env := []string{config.SystemOneAPIKeyEnv + "=" + mockAskKey, config.SystemOneKeyOriginEnv + "="}

	_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, env, fixtureFilename)
	if exitCode != int(cli.ExitConfig) {
		t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitConfig, stderr)
	}

	want := mock.URL + " (node breaks-a-rule) needs a key, and " + config.SystemOneAPIKeyEnv + " is sent only to https://api.typesafe.ai:443"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr missing %q, got: %s", want, stderr)
	}
}

func TestE2E_SystemOneAskNodeReadsBothSidesOfAChange(t *testing.T) {
	const gate = "\tgate() // " + mockAskStateTrigger + "\n"

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
			mock := startMockSystemOne(t)
			dir, binary := buildE2EBinary(t)
			writeE2EConfig(t, dir, mock.configYAML(askPipeline("")))
			writeRuledADRs(t, dir)
			commitFile(t, dir, "app.go", tt.before, "base")
			commitFile(t, dir, "app.go", tt.after, "change")

			stdout, stderr, exitCode := runCheckWithEnv(t, dir, binary, mock.env(), "--format", "json", "--since", "HEAD~1")
			if exitCode != int(cli.ExitSuccess) {
				t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitSuccess, stderr)
			}

			report := decodeCheckReport(t, stdout, stderr)

			if len(report.Nodes) != 2 || report.Nodes[1].Name != "breaks-a-rule" || report.Nodes[1].Received != 2 || report.Nodes[1].Kept != tt.wantKept {
				t.Errorf("nodes = %+v, want breaks-a-rule receiving 2 and keeping %d", report.Nodes, tt.wantKept)
			}
		})
	}
}

func TestE2E_SystemOneAskingTokensAreReported(t *testing.T) {
	for _, args := range [][]string{{"--format", "json"}, {"--format", "json", "--dry-run"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			mock := startMockSystemOne(t)
			tempDir, binaryPath := buildE2EBinary(t)
			writeE2EConfig(t, tempDir, mock.configYAML(askPipeline("")))
			writeRuledADRs(t, tempDir)
			writeFixture(t, tempDir, fixtureFilename, violationFixtureContent())

			stdout, stderr, _ := runCheckWithEnv(t, tempDir, binaryPath, mock.env(), append(args, fixtureFilename)...)

			var report usageReport
			if err := json.Unmarshal([]byte(stdout), &report); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %q\nstderr: %s", err, stdout, stderr)
			}

			asking := report.Usage.Asking
			if asking == nil || asking.Calls != 1 || asking.InputTokens == nil || *asking.InputTokens != mockAskInputTokens ||
				asking.OutputTokens == nil || *asking.OutputTokens != mockAskOutputTokens || asking.Cost != nil {
				t.Errorf("asking usage = %+v, want one call's tokens and no cost", asking)
			}
		})
	}
}
