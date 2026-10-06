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

// Printer is safe for concurrent use; a nil *Printer writes to os.Stderr, never stdout. Only
// Result lines are primary output: Err reports their first failed write.
type Printer struct {
	sink        *sink
	debug       bool
	indent      int
	parent      *Printer
	header      string
	headerStyle style
	// ownsBuffer is false for an Indented child, which writes through a buffer it doesn't own.
	ownsBuffer bool
}

type sink struct {
	mu        *sync.Mutex
	w         io.Writer
	buf       *bytes.Buffer
	midLine   bool
	color     bool
	hasResult bool
	failed    *error
}

var stderrSink = &sink{mu: &sync.Mutex{}}

func New(w io.Writer, debug bool, opts ...Option) *Printer {
	s := &sink{mu: &sync.Mutex{}, w: w, failed: new(error)}
	for _, opt := range opts {
		opt(s)
	}

	return &Printer{sink: s, debug: debug}
}

func (p *Printer) Err() error {
	out := p.out()

	out.mu.Lock()
	defer out.mu.Unlock()

	if out.failed == nil {
		return nil
	}

	return *out.failed
}

func Discard() *Printer {
	return New(io.Discard, false)
}

func (p *Printer) DebugEnabled() bool {
	return p != nil && p.debug
}

func (p *Printer) Result(format string, args ...any) { p.line(lineRole{result: true}, format, args...) }

func (p *Printer) Info(format string, args ...any) { p.line(lineRole{}, format, args...) }

func (p *Printer) Note(format string, args ...any) {
	p.line(lineRole{label: "Note: "}, format, args...)
}

func (p *Printer) Warn(format string, args ...any) {
	p.line(lineRole{label: "Warning: ", labelStyle: warnStyle}, format, args...)
}

func (p *Printer) Error(format string, args ...any) {
	p.line(lineRole{label: "Error: ", labelStyle: errorStyle}, format, args...)
}

func (p *Printer) Debug(format string, args ...any) {
	if p.DebugEnabled() {
		p.line(lineRole{label: "[DEBUG] ", lineStyle: debugStyle}, format, args...)
	}
}

func (p *Printer) Field(key, format string, args ...any) {
	p.line(lineRole{label: key + ": "}, format, args...)
}

// Group buffers its output until Flush, so one unit of work (a file) prints as
// a contiguous block even when units run in parallel.
func (p *Printer) Group(header string) *Printer {
	return p.group(header, fileStyle)
}

func (p *Printer) group(header string, headerStyle style) *Printer {
	g := &Printer{
		sink:        &sink{mu: p.out().mu, buf: &bytes.Buffer{}, failed: p.out().failed, color: p.out().color},
		debug:       p.DebugEnabled(),
		indent:      p.depth(),
		parent:      p,
		header:      header,
		headerStyle: headerStyle,

		ownsBuffer: true,
	}
	if header != "" {
		g.indent++
	}

	return g
}

// Indented prints one level deeper, straight through to p's destination.
func (p *Printer) Indented() *Printer {
	return &Printer{sink: p.out(), debug: p.DebugEnabled(), indent: p.depth() + 1}
}

func (p *Printer) Flush() {
	if p == nil || !p.ownsBuffer {
		return
	}

	p.sink.mu.Lock()

	var block strings.Builder

	if p.header != "" && p.sink.buf.Len() > 0 {
		header := strings.Split(p.header, "\n")
		for i, headerLine := range header {
			header[i] = p.sink.paint(p.headerStyle, headerLine)
		}

		block.WriteString(strings.Repeat(indentUnit, p.indent-1) + strings.Join(header, "\n") + "\n")
	}

	block.Write(p.sink.buf.Bytes())

	if p.sink.midLine {
		block.WriteString("\n")
	}

	result := p.sink.hasResult
	p.sink.buf.Reset()
	p.sink.midLine = false
	p.sink.hasResult = false
	p.sink.mu.Unlock()

	if block.Len() > 0 {
		p.parent.write(block.String(), false, result)
	}
}

type Progress struct{ p *Printer }

func (p *Printer) Progress() *Progress { return &Progress{p: p} }

func (pr *Progress) Tick() { pr.p.write(".", true, false) }

func (pr *Progress) Done() { pr.p.write("", false, false) }

type lineRole struct {
	label      string
	labelStyle style
	lineStyle  style
	result     bool
}

// Each line opens and resets its own color, so no escape sequence spans a newline.
func (p *Printer) line(role lineRole, format string, args ...any) {
	out := p.out()
	prefix := strings.Repeat(indentUnit, p.depth())
	continuation := strings.Repeat(" ", len(role.label))
	lines := strings.Split(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"), "\n")

	var block strings.Builder

	block.WriteString(prefix + out.paint(role.lineStyle, out.paint(role.labelStyle, role.label)+lines[0]) + "\n")

	for _, text := range lines[1:] {
		block.WriteString(prefix + out.paint(role.lineStyle, continuation+text) + "\n")
	}

	p.write(block.String(), false, role.result)
}

// write ends a pending progress line before any full line, so a warning printed
// mid-progress starts on its own line. A group's result write error is recorded at Flush.
func (p *Printer) write(text string, partial, result bool) {
	out := p.out()

	out.mu.Lock()
	defer out.mu.Unlock()

	dest := out.dest()

	var err error
	if out.midLine && !partial {
		_, err = io.WriteString(dest, "\n")
	}

	if err == nil {
		_, err = io.WriteString(dest, text)
	}

	out.midLine = partial

	switch {
	case !result:
	case out.buf != nil:
		out.hasResult = true
	case err != nil && out.failed != nil && *out.failed == nil:
		*out.failed = err
	}
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
