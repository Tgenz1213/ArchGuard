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
