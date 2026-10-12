//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func reuseTestConfig(model string, dim int) string {
	return fmt.Sprintf(`
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  model: %q
  embedding_dim: %d
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted"]
`, model, dim)
}

func writeReuseTestADR(t *testing.T, dir, name, decision string) {
	t.Helper()

	content := fmt.Sprintf("---\ntitle: %q\nstatus: \"Accepted\"\nscope: \"**\"\n---\n\n## Decision\n%s\n", name, decision)
	if err := os.WriteFile(filepath.Join(dir, "docs", "arch", name+".md"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write ADR %s: %v", name, err)
	}
}

// Steps run in order against one repository, each starting from the index the previous one saved.
func TestE2E_IndexRebuildEmbedsOnlyWhatChanged(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reuseTestConfig("model-a", 768))

	if err := os.MkdirAll(filepath.Join(tempDir, "docs", "arch"), 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}

	for _, name := range []string{"0001-first", "0002-second", "0003-third"} {
		writeReuseTestADR(t, tempDir, name, "Original decision.")
	}

	if err := os.WriteFile(filepath.Join(tempDir, "clean.js"), []byte("function greet() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	indexPath := filepath.Join(tempDir, ".archguard", "index.json")

	steps := []struct {
		name       string
		change     func(t *testing.T)
		command    string
		wantEmbeds int
	}{
		{name: "first index embeds every ADR", change: func(*testing.T) {}, command: "index", wantEmbeds: 3},
		{name: "unchanged index embeds nothing", change: func(*testing.T) {}, command: "index", wantEmbeds: 0},
		{
			name:       "index after one edit embeds that ADR",
			change:     func(t *testing.T) { writeReuseTestADR(t, tempDir, "0002-second", "Edited decision.") },
			command:    "index",
			wantEmbeds: 1,
		},
		{
			name:       "check rebuilding after one edit embeds that ADR",
			change:     func(t *testing.T) { writeReuseTestADR(t, tempDir, "0003-third", "Edited decision.") },
			command:    "check",
			wantEmbeds: 1,
		},
		{
			name:       "different model embeds every ADR",
			change:     func(t *testing.T) { writeE2EConfig(t, tempDir, reuseTestConfig("model-b", 768)) },
			command:    "index",
			wantEmbeds: 3,
		},
		{
			name:       "different dimension embeds every ADR",
			change:     func(t *testing.T) { writeE2EConfig(t, tempDir, reuseTestConfig("model-b", 512)) },
			command:    "index",
			wantEmbeds: 3,
		},
		{
			name: "corrupt saved index embeds every ADR",
			change: func(t *testing.T) {
				if err := os.WriteFile(indexPath, []byte("{not json"), 0644); err != nil {
					t.Fatalf("Failed to corrupt index: %v", err)
				}
			},
			command:    "index",
			wantEmbeds: 3,
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			step.change(t)

			var output string
			if step.command == "check" {
				output = runCheckCapture(t, tempDir, binaryPath, "clean.js", int(cli.ExitSuccess))
			} else {
				output = runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))
			}

			if got := strings.Count(output, testutil.MockEmbedDocumentMarker); got != step.wantEmbeds {
				t.Fatalf("ADR embeddings = %d, want %d. Output: %s", got, step.wantEmbeds, output)
			}
		})
	}
}

// Steps run in order against one repository, each starting from the index the previous one saved.
func TestE2E_CheckRebuildLogsOneLineWithItsReason(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reuseTestConfig("model-a", 768))

	if err := os.MkdirAll(filepath.Join(tempDir, "docs", "arch"), 0755); err != nil {
		t.Fatalf("Failed to create ADR directory: %v", err)
	}

	writeReuseTestADR(t, tempDir, "0001-first", "Original decision.")

	if err := os.WriteFile(filepath.Join(tempDir, "clean.js"), []byte("function greet() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	steps := []struct {
		name       string
		change     func(t *testing.T)
		debug      bool
		wantReason string
	}{
		{name: "missing index", change: func(*testing.T) {}, wantReason: "(index missing)"},
		{
			name:       "edited ADR",
			change:     func(t *testing.T) { writeReuseTestADR(t, tempDir, "0001-first", "Edited decision.") },
			wantReason: "(ADRs changed)",
		},
		{
			name:       "different model",
			change:     func(t *testing.T) { writeE2EConfig(t, tempDir, reuseTestConfig("model-b", 768)) },
			wantReason: "(embedding model changed)",
		},
		{
			name:       "debug shows the hashes",
			change:     func(t *testing.T) { writeReuseTestADR(t, tempDir, "0001-first", "Edited again.") },
			debug:      true,
			wantReason: "(ADRs changed)",
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			step.change(t)

			args := []string{"clean.js"}
			if step.debug {
				args = append(args, "--debug")
			}

			_, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, args...)
			if exitCode != int(cli.ExitSuccess) {
				t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitSuccess, stderr)
			}

			if want := "Rebuilt the ADR index " + step.wantReason + ": embedded 1 ADR(s)."; !strings.Contains(stderr, want) {
				t.Errorf("expected %q. stderr: %s", want, stderr)
			}

			if strings.Contains(stderr, "saved hash") != step.debug {
				t.Errorf("hashes shown = %v, want %v. stderr: %s", !step.debug, step.debug, stderr)
			}

			if !step.debug && strings.Contains(stderr, "Generating embeddings") {
				t.Errorf("expected only the one rebuild line without --debug. stderr: %s", stderr)
			}

			if strings.Contains(stderr, "\n.") {
				t.Errorf("progress dots printed to a log that is not a terminal. stderr: %s", stderr)
			}
		})
	}
}

func TestE2E_CheckReportsAnADRThatFailedToEmbed(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reuseTestConfig("model-a", 768))
	writeNoSecretsADR(t, tempDir)

	failingADR := filepath.Join(tempDir, "docs", "arch", "0001-always-fails.md")
	if err := os.WriteFile(failingADR, []byte(alwaysFailsToEmbedADRContent), 0644); err != nil {
		t.Fatalf("Failed to create failing ADR: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tempDir, "clean.js"), []byte("function greet() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	for _, run := range []string{"rebuilding", "steady state"} {
		t.Run(run, func(t *testing.T) {
			stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "clean.js")
			if exitCode != int(cli.ExitSuccess) {
				t.Fatalf("exit code %d, want %d. stderr: %s", exitCode, cli.ExitSuccess, stderr)
			}

			if !strings.Contains(stdout, "0001-always-fails.md") {
				t.Errorf("expected the report to list the ADR that is not in the index. stdout: %s", stdout)
			}

			if strings.Contains(stdout, "0000-no-secrets-in-log.md") {
				t.Errorf("an indexed ADR was reported as missing. stdout: %s", stdout)
			}
		})
	}

	_, stderr, _ := runCheckJSON(t, tempDir, binaryPath, "clean.js")
	if !strings.Contains(stderr, "not in the index") {
		t.Errorf("expected --format json to warn about the ADR that is not in the index. stderr: %s", stderr)
	}
}
