//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

func adrIDPatternConfig(pattern string) string {
	return sinceConfig + "  adr_id_pattern: '" + pattern + "'\n"
}

func TestE2E_ADRIDPattern_TwoCaptureGroupsIsAConfigError(t *testing.T) {
	dir, binary := buildE2EBinary(t)
	writeE2EConfig(t, dir, adrIDPatternConfig(`^(\d+)-(\w+)`))
	writeNoSecretsADR(t, dir)

	output := runIndexCmdCapture(t, dir, binary, int(cli.ExitConfig))
	if !strings.Contains(output, "analysis.adr_id_pattern") {
		t.Errorf("the error must name analysis.adr_id_pattern. Output:\n%s", output)
	}
}

func TestE2E_ADRIDPattern_MatchingNoFileWarnsButRuns(t *testing.T) {
	dir, binary := buildE2EBinary(t)
	writeE2EConfig(t, dir, adrIDPatternConfig(`^adr-(\d+)-`))
	writeNoSecretsADR(t, dir)

	if err := os.WriteFile(filepath.Join(dir, fixtureFilename), []byte(violationFixtureContent()), 0o644); err != nil {
		t.Fatalf("write %s: %v", fixtureFilename, err)
	}

	if output := runIndexCmdCapture(t, dir, binary, int(cli.ExitSuccess)); !strings.Contains(output, "adr_id_pattern") {
		t.Errorf("index must warn that the pattern matches no ADR file. Output:\n%s", output)
	}

	stdout, stderr, code := runCheckJSON(t, dir, binary, fixtureFilename)
	if code != int(cli.ExitDriftDetected) {
		t.Fatalf("exit = %d, want %d. Stderr:\n%s", code, cli.ExitDriftDetected, stderr)
	}

	if !strings.Contains(stderr, "adr_id_pattern") {
		t.Errorf("check must warn on stderr. Stderr:\n%s", stderr)
	}

	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout must stay one JSON document:\n%s", stdout)
	}
}

func TestE2E_ADRIDPattern_MatchingAFileDoesNotWarn(t *testing.T) {
	dir, binary := buildE2EBinary(t)
	writeE2EConfig(t, dir, adrIDPatternConfig(`^(\d+)-`))
	writeNoSecretsADR(t, dir)

	if output := runIndexCmdCapture(t, dir, binary, int(cli.ExitSuccess)); strings.Contains(output, "adr_id_pattern") {
		t.Errorf("a pattern that matches an ADR file must not warn. Output:\n%s", output)
	}
}
