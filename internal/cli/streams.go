package cli

import (
	"fmt"
	"os"

	"github.com/tgenz1213/archguard/internal/output"
)

type streamColors struct{ stdout, stderr bool }

// Decided once per run; restore puts a Windows console back the way the run found it.
func decideColors(mode output.ColorMode) (colors streamColors, restore func()) {
	stdout, restoreStdout := output.ColorFor(mode, os.Stdout)
	stderr, restoreStderr := output.ColorFor(mode, os.Stderr)

	return streamColors{stdout: stdout, stderr: stderr}, func() {
		restoreStderr()
		restoreStdout()
	}
}

func (c streamColors) stdoutPrinter(debug bool) *output.Printer {
	return output.New(os.Stdout, debug, output.WithColor(c.stdout))
}

func (c streamColors) stderrPrinter(debug bool) *output.Printer {
	return output.New(os.Stderr, debug, output.WithColor(c.stderr))
}

func outputWriteError(err error) error {
	return fmt.Errorf("failed to write output: %w", err)
}
