//go:build e2e

package e2e

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cli"
	"github.com/tgenz1213/archguard/internal/testutil"
)

func TestE2E_RunThatCheckedNothingBecauseOfErrors(t *testing.T) {
	tempDir, binaryPath := buildE2EBinary(t)

	writeE2EConfig(t, tempDir, reportE2EConfig)
	writeNoSecretsADR(t, tempDir)

	const failing, clean = "failing.js", "clean.js"

	files := map[string]string{
		failing: fmt.Sprintf("function f() {\n    console.log(%q);\n}\n", testutil.MockEmbedFailureTrigger),
		clean:   "function ok() { return 1; }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create fixture: %v", err)
		}
	}

	gitAdd(t, tempDir, failing)
	runIndexCmd(t, tempDir, binaryPath, int(cli.ExitSuccess))

	t.Run("every file skipped: exit 8, the report still lists the file", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, failing)

		if exitCode != int(cli.ExitNothingChecked) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitNothingChecked, stdout, stderr)
		}

		if !strings.Contains(stdout, failing) || !strings.Contains(stderr, "Error:") {
			t.Errorf("want the skipped file in the report and an error line on stderr.\nstdout: %s\nstderr: %s", stdout, stderr)
		}
	})

	t.Run("another file checked: skips alone do not fail the run", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, failing, clean)

		if exitCode != int(cli.ExitSuccess) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitSuccess, stdout, stderr)
		}

		if !strings.Contains(stdout, failing) {
			t.Errorf("the skipped file is missing from the report:\n%s", stdout)
		}
	})

	t.Run("--update-baseline: exit 8 and no baseline written", func(t *testing.T) {
		stdout, stderr, exitCode := runCheckWithEnv(t, tempDir, binaryPath, nil, "--update-baseline")

		if exitCode != int(cli.ExitNothingChecked) {
			t.Fatalf("exit code %d, want %d. stdout: %s stderr: %s", exitCode, cli.ExitNothingChecked, stdout, stderr)
		}

		if _, err := os.Stat(filepath.Join(tempDir, baseline.Path)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("baseline file stat err = %v, want it not written", err)
		}
	})
}
