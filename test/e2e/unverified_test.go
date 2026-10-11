//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

const unverifiedFixture = "invented.js"

func TestE2E_UnverifiedFindings(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reportE2EConfig)
	writeNoSecretsADR(t, tempDir)

	files := map[string]string{
		fixtureFilename:   violationFixtureContent(),
		unverifiedFixture: "// " + testutil.MockUnverifiedTrigger + "\nfunction ok() { return 1; }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create fixture: %v", err)
		}
	}

	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("only unverified findings: listed apart and the run passes", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, unverifiedFixture)

		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitSuccess, stdout, stderr)
		}

		if !strings.Contains(stdout, "[UNVERIFIED]") || strings.Contains(stdout, "[VIOLATION]") {
			t.Errorf("report does not list the finding as unverified only:\n%s", stdout)
		}
	})

	t.Run("only unverified findings in JSON: listed apart, count 0", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckJSON(t, tempDir, binaryPath, unverifiedFixture)

		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitSuccess, stdout, stderr)
		}

		report := decodeFindings(t, stdout)
		if report.Count != 0 || len(report.Violations) != 0 || len(report.Unverified) != 1 || report.Unverified[0].File != unverifiedFixture {
			t.Errorf("report = %+v, want one unverified finding and no violation", report)
		}
	})

	t.Run("a verified violation still fails the run", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--format", "json", fixtureFilename, unverifiedFixture)

		if exitCode != int(cli.ExitDriftDetected) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitDriftDetected, stdout, stderr)
		}

		report := decodeFindings(t, stdout)
		if report.Count != 1 || len(report.Violations) != 1 || report.Violations[0].File != fixtureFilename || len(report.Unverified) != 1 {
			t.Errorf("report = %+v, want one violation and one unverified finding", report)
		}
	})
}

type findingsReport struct {
	Count      int `json:"count"`
	Violations []struct {
		File string `json:"file"`
	} `json:"violations"`
	Unverified []struct {
		File string `json:"file"`
	} `json:"unverified"`
}

func decodeFindings(t *testing.T, stdout string) findingsReport {
	t.Helper()

	var report findingsReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}

	return report
}
