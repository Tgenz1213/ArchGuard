package output_test

import (
	"io"
	"os"
	"testing"

	"github.com/muesli/termenv"

	"github.com/tgenz1213/archguard/internal/output"
)

type fakeEnv map[string]string

func (e fakeEnv) Environ() []string {
	var env []string
	for k, v := range e {
		env = append(env, k+"="+v)
	}

	return env
}

func (e fakeEnv) Getenv(key string) string { return e[key] }

func TestColorEnabled(t *testing.T) {
	colorTerm := fakeEnv{"TERM": "xterm-256color", "COLORTERM": "truecolor"}

	tests := []struct {
		name     string
		mode     output.ColorMode
		terminal bool
		env      fakeEnv
		want     bool
	}{
		{"auto on a terminal", output.ColorAuto, true, colorTerm, true},
		{"auto off a terminal", output.ColorAuto, false, colorTerm, false},
		{"auto with NO_COLOR", output.ColorAuto, true, fakeEnv{"TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "1"}, false},
		{"auto with empty NO_COLOR", output.ColorAuto, true, fakeEnv{"TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": ""}, true},
		{"auto with TERM=dumb", output.ColorAuto, true, fakeEnv{"TERM": "dumb"}, false},
		{"always off a terminal", output.ColorAlways, false, fakeEnv{"NO_COLOR": "1", "TERM": "dumb"}, true},
		{"never on a terminal", output.ColorNever, true, colorTerm, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := termenv.NewOutput(io.Discard, termenv.WithEnvironment(tt.env), termenv.WithTTY(tt.terminal))

			if got := output.ColorEnabled(tt.mode, out, tt.env.Getenv); got != tt.want {
				t.Errorf("ColorEnabled(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestColorForPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = r.Close() //nolint:errcheck // test cleanup
		_ = w.Close() //nolint:errcheck // test cleanup
	})

	if output.ColorFor(output.ColorAuto, w) {
		t.Error("ColorFor(auto, pipe) = true, want false")
	}

	if !output.ColorFor(output.ColorAlways, w) {
		t.Error("ColorFor(always, pipe) = false, want true")
	}
}
