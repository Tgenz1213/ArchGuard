package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const indentUnit = "  "

// Printer is safe for concurrent use. A nil *Printer writes to os.Stderr so
// output nobody wired up can never land in a JSON report on stdout.
type Printer struct {
	sink   *sink
	debug  bool
	indent int
	parent *Printer
	header string
}

type sink struct {
	mu      *sync.Mutex
	w       io.Writer
	buf     *bytes.Buffer
	midLine bool
}

var stderrSink = &sink{mu: &sync.Mutex{}}

func New(w io.Writer, debug bool) *Printer {
	return &Printer{sink: &sink{mu: &sync.Mutex{}, w: w}, debug: debug}
}

func Discard() *Printer {
	return New(io.Discard, false)
}

func (p *Printer) DebugEnabled() bool {
	return p != nil && p.debug
}

func (p *Printer) Info(format string, args ...any) { p.line("", format, args...) }

func (p *Printer) Note(format string, args ...any) { p.line("Note: ", format, args...) }

func (p *Printer) Warn(format string, args ...any) { p.line("Warning: ", format, args...) }

func (p *Printer) Error(format string, args ...any) { p.line("Error: ", format, args...) }

func (p *Printer) Debug(format string, args ...any) {
	if p.DebugEnabled() {
		p.line("[DEBUG] ", format, args...)
	}
}

func (p *Printer) Field(key, format string, args ...any) {
	p.line(key+": ", format, args...)
}

// Group buffers its output until Flush, so one unit of work (a file) prints as
// a contiguous block even when units run in parallel.
func (p *Printer) Group(header string) *Printer {
	g := &Printer{
		sink:   &sink{mu: p.out().mu, buf: &bytes.Buffer{}},
		debug:  p.DebugEnabled(),
		indent: p.depth(),
		parent: p,
		header: header,
	}
	if header != "" {
		g.indent++
	}

	return g
}

func (p *Printer) Flush() {
	if p == nil || p.sink.buf == nil {
		return
	}

	p.sink.mu.Lock()

	var block strings.Builder
	if p.header != "" && p.sink.buf.Len() > 0 {
		block.WriteString(strings.Repeat(indentUnit, p.indent-1) + p.header + "\n")
	}

	block.Write(p.sink.buf.Bytes())

	if p.sink.midLine {
		block.WriteString("\n")
	}

	p.sink.buf.Reset()
	p.sink.midLine = false
	p.sink.mu.Unlock()

	if block.Len() > 0 {
		p.parent.write(block.String(), false)
	}
}

type Progress struct{ p *Printer }

func (p *Printer) Progress() *Progress { return &Progress{p: p} }

func (pr *Progress) Tick() { pr.p.write(".", true) }

func (pr *Progress) Done() { pr.p.write("", false) }

type Violation struct {
	File           string
	Line           int
	Verified       bool
	Baselined      bool
	ADRID          string
	Title          string
	Reasoning      string
	Code           string
	Suggestion     string
	BaselineReason string
}

func (p *Printer) Violation(v Violation) {
	label := "VIOLATION"
	if v.Baselined {
		label = "BASELINED"
	}

	header := fmt.Sprintf("[%s] %s [Line %d]", label, v.Title, v.Line)
	if !v.Verified {
		header = fmt.Sprintf("[%s] %s [UNVERIFIED: quoted code not found in analyzed content]", label, v.Title)
	}

	details := p.Group(header)
	details.Field("Reasoning", "%s", v.Reasoning)

	if v.Code != "" {
		details.Field("Code", "%s", v.Code)
	}

	if v.Suggestion != "" {
		details.Field("Suggestion (unverified)", "%s", v.Suggestion)
	}

	if v.BaselineReason != "" {
		details.Field("Baseline Reason", "%s", v.BaselineReason)
	}

	details.Flush()
}

func (p *Printer) line(label, format string, args ...any) {
	prefix := strings.Repeat(indentUnit, p.depth())
	continuation := prefix + strings.Repeat(" ", len(label))
	lines := strings.Split(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"), "\n")

	var b strings.Builder

	b.WriteString(prefix + label + lines[0] + "\n")

	for _, l := range lines[1:] {
		b.WriteString(continuation + l + "\n")
	}

	p.write(b.String(), false)
}

// write ends a pending progress line before any full line, so a warning printed
// mid-progress starts on its own line.
func (p *Printer) write(s string, partial bool) {
	out := p.out()

	out.mu.Lock()
	defer out.mu.Unlock()

	w := out.dest()
	if out.midLine && !partial {
		_, _ = io.WriteString(w, "\n")
	}

	_, _ = io.WriteString(w, s)
	out.midLine = partial
}

func (p *Printer) out() *sink {
	if p == nil {
		return stderrSink
	}

	return p.sink
}

func (p *Printer) depth() int {
	if p == nil {
		return 0
	}

	return p.indent
}

func (s *sink) dest() io.Writer {
	switch {
	case s.buf != nil:
		return s.buf
	case s.w != nil:
		return s.w
	default:
		return os.Stderr
	}
}
