package output

import (
	"os"

	"github.com/muesli/termenv"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// ColorFor decides whether output written to f gets color. On a Windows console it switches
// on escape-sequence support, which outlives the process, so call restore once output is done.
func ColorFor(mode ColorMode, f *os.File) (on bool, restore func()) {
	restore = func() {}

	out := termenv.NewOutput(f)
	if !ColorEnabled(mode, out, os.Getenv) {
		return false, restore
	}

	undo, err := termenv.EnableVirtualTerminalProcessing(out)
	if err == nil {
		restore = func() { _ = undo() } //nolint:errcheck // best-effort; the run's result is already decided
	}

	return err == nil || mode == ColorAlways, restore
}

// ColorEnabled is the policy behind ColorFor: an explicit mode wins over NO_COLOR and TERM.
func ColorEnabled(mode ColorMode, out *termenv.Output, getenv func(string) string) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}

	// termenv ignores TERM on Windows.
	if getenv("TERM") == "dumb" {
		return false
	}

	return out.EnvColorProfile() != termenv.Ascii
}
