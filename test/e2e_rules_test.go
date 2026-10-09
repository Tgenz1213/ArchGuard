//go:build e2e

package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

func TestE2E_RulesHeadingFromConfigReachesIndexAndCheck(t *testing.T) {
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
  accepted_statuses: ["Accepted"]
  rules_heading: "Detection"
`)

	adr := strings.Replace(noSecretsADRContent, "## Context", "## Detection\n\n- Never log secrets\n\n## Rules\n\n- Not the configured heading\n\n## Context", 1)

	adrDir := filepath.Join(tempDir, "docs", "arch")
	if err := os.MkdirAll(adrDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(adrDir, "0000-no-secrets-in-log.md"), []byte(adr), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tempDir, fixtureFilename), []byte(violationFixtureContent()), 0644); err != nil {
		t.Fatal(err)
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	data, err := os.ReadFile(filepath.Join(tempDir, ".archguard", "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	var index struct {
		ADRs []struct {
			Rules []struct {
				Statement string `json:"statement"`
			} `json:"rules"`
		} `json:"adrs"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}

	if len(index.ADRs) != 1 || len(index.ADRs[0].Rules) != 1 || index.ADRs[0].Rules[0].Statement != "Never log secrets" {
		t.Fatalf("index should hold only the rule under the configured heading, got %s", data)
	}

	output := runCheckCapture(t, tempDir, binaryPath, fixtureFilename, int(cli.ExitDriftDetected))
	if strings.Contains(output, "Triggering index rebuild") {
		t.Errorf("check resolved different rules than index, so it rebuilt a fresh index:\n%s", output)
	}
}
