//go:build e2e

package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

// Workflow commands start with "::"; their format is pinned in internal/output's tests.
func splitAnnotations(s string) (annotations int, rest string) {
	var kept []string

	for line := range strings.SplitAfterSeq(s, "\n") {
		if strings.HasPrefix(line, "::") {
			annotations++
		} else {
			kept = append(kept, line)
		}
	}

	return annotations, strings.Join(kept, "")
}

func TestE2E_GitHubAnnotations(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, `
version: "1"
llm:
  provider: "ollama"
vector_store:
  provider: "ollama"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`)
	writeNoSecretsADR(t, tempDir)

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	inActions := []string{"GITHUB_ACTIONS=true"}

	assertNoAnnotations := func(t *testing.T, output string) {
		t.Helper()

		if n, _ := splitAnnotations(output); n != 0 {
			t.Errorf("got %d annotations, want none:\n%s", n, output)
		}
	}

	t.Run("one annotation per new violation, report and exit code unchanged", func(t *testing.T) {
		plainOut, plainErr, plainCode := runCheckWithEnv(t, tempDir, binaryPath, []string{"GITHUB_ACTIONS=false"}, fixtureFilename)
		ghOut, ghErr, ghCode := runCheckWithEnv(t, tempDir, binaryPath, inActions, fixtureFilename)

		if plainCode != int(cli.ExitDriftDetected) || ghCode != plainCode {
			t.Fatalf("exit codes: without annotations %d, with %d; want both %d", plainCode, ghCode, cli.ExitDriftDetected)
		}

		assertNoAnnotations(t, plainOut+plainErr)
		assertNoAnnotations(t, ghOut)

		if n, _ := splitAnnotations(ghErr); n != 1 {
			t.Errorf("got %d annotations on stderr, want 1:\n%s", n, ghErr)
		}

		if ghOut != plainOut {
			t.Errorf("the report differs from a run without annotations.\nwith:\n%s\nwithout:\n%s", ghOut, plainOut)
		}
	})

	t.Run("none under --format json", func(t *testing.T) {
		stdout, stderr, code := runCheckWithEnv(t, tempDir, binaryPath, inActions, "--format", "json", fixtureFilename)
		if code != int(cli.ExitDriftDetected) {
			t.Fatalf("exit code %d, want %d", code, cli.ExitDriftDetected)
		}

		assertNoAnnotations(t, stdout+stderr)
	})
}
