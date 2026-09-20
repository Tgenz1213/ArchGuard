package stage

import (
	"fmt"
	"io"
)

type Debug interface {
	Enabled() bool
	Printf(format string, args ...any)
}

type writerDebug struct{ w io.Writer }

func NewDebug(w io.Writer) Debug { return writerDebug{w: w} }

func (d writerDebug) Enabled() bool { return true }

func (d writerDebug) Printf(format string, args ...any) { _, _ = fmt.Fprintf(d.w, format, args...) }

type noDebug struct{}

// NoDebug lets scorers call Debug unconditionally instead of nil-checking.
var NoDebug Debug = noDebug{}

func (noDebug) Enabled() bool { return false }

func (noDebug) Printf(string, ...any) {}
