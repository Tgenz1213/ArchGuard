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

// ColorFor decides whether output written to f gets color, and on a Windows console
// switches on the escape-sequence support that color needs.
func ColorFor(mode ColorMode, f *os.File) bool {
	out := termenv.NewOutput(f)
	if !ColorEnabled(mode, out, os.Getenv) {
		return false
	}

	_, err := termenv.EnableVirtualTerminalProcessing(out)

	return err == nil || mode == ColorAlways
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
