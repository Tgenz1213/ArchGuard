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

func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	commitAll(t, dir, message)
}

func checkSince(t *testing.T, dir, binary string, args ...string) (stdout string, code int) {
	t.Helper()

	stdout, _, code = runCheckWithEnv(t, dir, binary, nil, append([]string{"--ci", "--since", "HEAD~1"}, args...)...)

	return stdout, code
}

func reportedLines(t *testing.T, stdout string) []int {
	t.Helper()

	var report struct {
		Violations []struct {
			Line int `json:"line"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout)
	}

	lines := make([]int, 0, len(report.Violations))
	for _, v := range report.Violations {
		lines = append(lines, v.Line)
	}

	return lines
}

func TestE2E_Change_JudgesOnlyWhatTheChangeTouched(t *testing.T) {
	const oldLine = "\tlog(\"password: old\")\n"

	setup := func(t *testing.T) (dir, binary string) {
		dir, binary = buildE2EBinary(t)
		writeE2EConfig(t, dir, sinceConfig)
		writeNoSecretsADR(t, dir)
		commitFile(t, dir, "app.go", "package app\n\nfunc a() {\n"+oldLine+"\tlog(\"one\")\n}\n", "base")

		return dir, binary
	}

	t.Run("a violation on an unchanged line near an edit does not fail", func(t *testing.T) {
		dir, binary := setup(t)
		commitFile(t, dir, "app.go", "package app\n\nfunc a() {\n"+oldLine+"\tlog(\"two\")\n}\n", "edit a neighbour")
		runIndexCmd(t, dir, binary, int(cli.ExitSuccess))

		if _, code := checkSince(t, dir, binary); code != int(cli.ExitSuccess) {
			t.Fatalf("exit = %d, want %d: the old line is context, not part of the change", code, cli.ExitSuccess)
		}
	})

	t.Run("a violation on an added line fails and points at that line", func(t *testing.T) {
		dir, binary := setup(t)
		commitFile(t, dir, "app.go", "package app\n\nfunc a() {\n"+oldLine+"\tlog(\"one\")\n\tlog(\"password: new\")\n}\n", "add a violation")
		runIndexCmd(t, dir, binary, int(cli.ExitSuccess))

		stdout, code := checkSince(t, dir, binary, "--format", "json")
		if code != int(cli.ExitDriftDetected) {
			t.Fatalf("exit = %d, want %d", code, cli.ExitDriftDetected)
		}

		if got := reportedLines(t, stdout); len(got) != 1 || got[0] != 6 {
			t.Fatalf("lines = %v, want [6]", got)
		}
	})

	t.Run("deleting code the ADR requires is reported where it was removed", func(t *testing.T) {
		dir, binary := buildE2EBinary(t)
		writeE2EConfig(t, dir, sinceConfig)
		writeNoSecretsADR(t, dir)
		required := "\tcheck() // " + testutil.MockRequiredMarker + "\n"
		commitFile(t, dir, "app.go", "package app\n\nfunc a() {\n"+required+"\tlog(\"one\")\n}\n", "base")
		commitFile(t, dir, "app.go", "package app\n\nfunc a() {\n\tlog(\"one\")\n}\n", "delete the required call")
		runIndexCmd(t, dir, binary, int(cli.ExitSuccess))

		stdout, code := checkSince(t, dir, binary, "--format", "json")
		if code != int(cli.ExitDriftDetected) {
			t.Fatalf("exit = %d, want %d", code, cli.ExitDriftDetected)
		}

		if got := reportedLines(t, stdout); len(got) != 1 || got[0] != 4 {
			t.Fatalf("lines = %v, want [4], the line the removed code sat in front of", got)
		}
	})

	t.Run("an ignore directive outside the diff still suppresses the ADR", func(t *testing.T) {
		dir, binary := buildE2EBinary(t)
		writeE2EConfig(t, dir, sinceConfig)
		writeNoSecretsADR(t, dir)

		body := "// archguard-ignore: 0000\npackage app\n\nfunc a() {\n" + strings.Repeat("\tlog(\"pad\")\n", 150) + "}\n"
		commitFile(t, dir, "app.go", body, "base")
		commitFile(t, dir, "app.go", strings.Replace(body, "}\n", "\tlog(\"password: new\")\n}\n", 1), "add a violation far from the header")
		runIndexCmd(t, dir, binary, int(cli.ExitSuccess))

		if _, code := checkSince(t, dir, binary); code != int(cli.ExitSuccess) {
			t.Fatalf("exit = %d, want %d: the header is more than 100 lines away, so it is not in the diff", code, cli.ExitSuccess)
		}
	})
}
