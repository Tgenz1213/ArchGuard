package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const (
	chatProviderMarker  = testutil.MockChatProviderMarker
	embedProviderMarker = testutil.MockEmbedProviderMarker
)

// Drives index+check against differing llm.provider/vector_store.provider
// names, asserting via stdout markers -- printed when a mock method is
// actually invoked, not when the provider is constructed -- that index
// only ever calls the embed mock and check calls both.
func TestE2E_DualProvider_IndexAndCheckUseDistinctProviders(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "claude"
vector_store:
  provider: "openai"
  embedding_dim: 768
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`
	writeE2EConfig(t, tempDir, configContent)
	writeNoSecretsADR(t, tempDir)

	t.Run("Index uses only the embed provider", func(t *testing.T) {
		output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitSuccess))

		if !strings.Contains(output, embedProviderMarker) {
			t.Fatalf("expected index output to contain %q, got: %s", embedProviderMarker, output)
		}
		if strings.Contains(output, chatProviderMarker) {
			t.Fatalf("index should never invoke the chat provider when providers differ, but output contains %q: %s", chatProviderMarker, output)
		}
	})

	fixturePath := filepath.Join(tempDir, fixtureFilename)
	if err := os.WriteFile(fixturePath, []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatalf("Failed to create fixture: %v", err)
	}

	t.Run("Check with a violation uses both providers", func(t *testing.T) {
		output := runCheckCapture(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))

		if !strings.Contains(output, chatProviderMarker) {
			t.Fatalf("expected check output to contain %q, got: %s", chatProviderMarker, output)
		}
		if !strings.Contains(output, embedProviderMarker) {
			t.Fatalf("expected check output to contain %q, got: %s", embedProviderMarker, output)
		}
	})

	t.Run("Check without a violation still succeeds end-to-end", func(t *testing.T) {
		if err := os.Remove(fixturePath); err != nil {
			t.Fatalf("Failed to remove fixture: %v", err)
		}
		runCheck(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitSuccess))
	})
}

// Proves llm.provider: claude with vector_store.provider unset exits ExitConfig before any provider is constructed.
func TestE2E_DualProvider_ConfigValidationFailsFast(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	configContent := `
version: "1"
llm:
  provider: "claude"
analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
`
	writeE2EConfig(t, tempDir, configContent)

	output := runIndexCmdCapture(t, tempDir, binaryPath, int(cli.ExitConfig))

	if strings.Contains(output, chatProviderMarker) || strings.Contains(output, embedProviderMarker) {
		t.Fatalf("expected no provider to be constructed before config validation fails, got output: %s", output)
	}
}
