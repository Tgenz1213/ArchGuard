package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func TestPrinterLabels(t *testing.T) {
	tests := []struct {
		name  string
		debug bool
		print func(p *output.Printer)
		want  string
	}{
		{"info", false, func(p *output.Printer) { p.Info("found %d", 2) }, "found 2\n"},
		{"note", false, func(p *output.Printer) { p.Note("ignoring %s", "x") }, "Note: ignoring x\n"},
		{"warn", false, func(p *output.Printer) { p.Warn("skipping %s", "a.md") }, "Warning: skipping a.md\n"},
		{"error", false, func(p *output.Printer) { p.Error("boom") }, "Error: boom\n"},
		{"field", false, func(p *output.Printer) { p.Field("Context mode", "%s", "diff") }, "Context mode: diff\n"},
		{"debug on", true, func(p *output.Printer) { p.Debug("mode") }, "[DEBUG] mode\n"},
		{"debug off", false, func(p *output.Printer) { p.Debug("mode") }, ""},
		{"trailing newline dropped", false, func(p *output.Printer) { p.Info("done\n") }, "done\n"},
		{"multi-line aligned under label", false, func(p *output.Printer) { p.Warn("a\nb") }, "Warning: a\n         b\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			tt.print(output.New(&buf, tt.debug))

			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupIndentsUnderHeaderAndPrintsOnFlush(t *testing.T) {
	var buf bytes.Buffer

	p := output.New(&buf, false)
	g := p.Group("a.go")
	g.Warn("slow")
	inner := g.Group("ADR 7")
	inner.Info("checked")
	inner.Flush()

	if buf.Len() != 0 {
		t.Fatalf("group wrote before Flush: %q", buf.String())
	}

	g.Flush()

	want := "a.go\n  Warning: slow\n  ADR 7\n    checked\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGroupWithoutHeaderKeepsIndent(t *testing.T) {
	var buf bytes.Buffer

	g := output.New(&buf, false).Group("")
	g.Info("x")
	g.Flush()

	if got := buf.String(); got != "x\n" {
		t.Errorf("got %q, want %q", got, "x\n")
	}
}

func TestIndentedWritesThroughOneLevelDeeper(t *testing.T) {
	var buf bytes.Buffer

	p := output.New(&buf, false)
	p.Info("summary")
	section := p.Indented()
	section.Info("Skipped: 1")

	if got := buf.String(); got != "summary\n  Skipped: 1\n" {
		t.Fatalf("got %q before any flush", got)
	}

	section.Indented().Info("- a.md")

	if got := buf.String(); got != "summary\n  Skipped: 1\n    - a.md\n" {
		t.Errorf("got %q", got)
	}
}

func TestFlushOnIndentedChildOfGroupDoesNotFlushTheGroup(t *testing.T) {
	var buf bytes.Buffer

	g := output.New(&buf, false).Group("a.go")
	sub := g.Indented()
	sub.Info("x")
	sub.Flush()

	if buf.Len() != 0 {
		t.Fatalf("Indented child flushed its group early: %q", buf.String())
	}

	g.Flush()

	if got := buf.String(); got != "a.go\n    x\n" {
		t.Errorf("got %q", got)
	}
}

func TestEmptyGroupPrintsNothing(t *testing.T) {
	var buf bytes.Buffer

	output.New(&buf, false).Group("a.go").Flush()

	if buf.Len() != 0 {
		t.Errorf("empty group printed %q", buf.String())
	}
}

func TestGroupInheritsDebug(t *testing.T) {
	var buf bytes.Buffer

	g := output.New(&buf, true).Group("")
	g.Debug("cache miss")
	g.Flush()

	if got := buf.String(); got != "[DEBUG] cache miss\n" {
		t.Errorf("got %q", got)
	}
}

func TestParallelGroupsDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer

	p := output.New(&buf, false)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			g := p.Group(fmt.Sprintf("file%d", i))
			for range 5 {
				g.Info("line")
			}

			g.Flush()
		})
	}

	wg.Wait()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 20*6 {
		t.Fatalf("got %d lines, want %d", len(lines), 20*6)
	}

	for i := 0; i < len(lines); i += 6 {
		if !strings.HasPrefix(lines[i], "file") {
			t.Fatalf("line %d = %q, want a header", i, lines[i])
		}

		for _, l := range lines[i+1 : i+6] {
			if l != "  line" {
				t.Fatalf("block starting %q interleaved: %q", lines[i], l)
			}
		}
	}
}

func TestProgressEndsLineBeforeNextMessage(t *testing.T) {
	var buf bytes.Buffer

	p := output.New(&buf, false)
	pr := p.Progress()
	pr.Tick()
	pr.Tick()
	p.Warn("skipping a.md")
	pr.Tick()
	pr.Done()
	pr.Done()

	want := "..\nWarning: skipping a.md\n.\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFlushEndsAGroupsUnfinishedProgressLine(t *testing.T) {
	var buf bytes.Buffer

	p := output.New(&buf, false)
	g := p.Group("a.go")
	g.Progress().Tick()
	g.Flush()
	p.Info("next")

	want := "a.go\n.\nnext\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestViolation(t *testing.T) {
	tests := []struct {
		name string
		v    output.Violation
		want string
	}{
		{
			name: "verified with all fields",
			v: output.Violation{
				Line: 12, Verified: true, Title: "No globals", Reasoning: "uses a global",
				Code: "var x", Suggestion: "inject it", BaselineReason: "accepted-debt",
			},
			want: "[VIOLATION] No globals [Line 12]\n  Reasoning: uses a global\n  Code: var x\n" +
				"  Suggestion (unverified): inject it\n  Baseline Reason: accepted-debt\n",
		},
		{
			name: "baselined and unverified",
			v:    output.Violation{Baselined: true, Title: "No globals", Reasoning: "r"},
			want: "[BASELINED] No globals [UNVERIFIED: quoted code not found in analyzed content]\n  Reasoning: r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			output.New(&buf, false).Violation(tt.v)

			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNilPrinterWritesToStderr(t *testing.T) {
	got := captureStderr(t, func() {
		var p *output.Printer

		p.Warn("unwired")
		p.Debug("hidden")

		g := p.Group("a.go")
		g.Info("x")
		g.Flush()
	})

	want := "Warning: unwired\na.go\n  x\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNewWithNilWriterWritesToStderr(t *testing.T) {
	got := captureStderr(t, func() { output.New(nil, false).Info("x") })

	if got != "x\n" {
		t.Errorf("got %q, want %q", got, "x\n")
	}
}

func TestDiscard(t *testing.T) {
	got := captureStderr(t, func() { output.Discard().Error("gone") })

	if got != "" {
		t.Errorf("Discard wrote %q to stderr", got)
	}
}

type failingWriter struct{}

var errWrite = errors.New("write failed")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestErrReportsFailedResultWrites(t *testing.T) {
	tests := []struct {
		name    string
		print   func(p *output.Printer)
		wantErr bool
	}{
		{"result line", func(p *output.Printer) { p.Result("1 new violation(s)") }, true},
		{"indented result line", func(p *output.Printer) { p.Indented().Result("- a.md") }, true},
		{"violation", func(p *output.Printer) { p.Violation(output.Violation{Title: "t", Verified: true}) }, true},
		{"group holding a violation", func(p *output.Printer) {
			g := p.Group("a.go")
			g.Violation(output.Violation{Title: "t", Verified: true})
			g.Flush()
		}, true},
		{"diagnostics", func(p *output.Printer) {
			p.Info("found")
			p.Note("n")
			p.Warn("w")
			p.Error("e")
			p.Debug("d")
			pr := p.Progress()
			pr.Tick()
			pr.Done()
		}, false},
		{"group without a result", func(p *output.Printer) {
			g := p.Group("a.go")
			g.Warn("slow")
			g.Flush()
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := output.New(failingWriter{}, true)
			tt.print(p)

			err := p.Err()
			if tt.wantErr && !errors.Is(err, errWrite) {
				t.Fatalf("Err() = %v, want %v", err, errWrite)
			}

			if !tt.wantErr && err != nil {
				t.Fatalf("Err() = %v, want nil for best-effort output", err)
			}
		})
	}
}

func TestErrIsNilAfterSuccessfulResults(t *testing.T) {
	var buf bytes.Buffer

	p := output.New(&buf, false)
	p.Result("ok")

	if err := p.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}

	var nilPrinter *output.Printer
	if err := nilPrinter.Err(); err != nil {
		t.Fatalf("nil Printer Err() = %v, want nil", err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stderr
	os.Stderr = w

	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	return string(out)
}
