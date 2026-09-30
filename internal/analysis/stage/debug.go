package stage

// Debug is satisfied by *output.Printer; each call prints one line.
type Debug interface {
	DebugEnabled() bool
	Debug(format string, args ...any)
}

type noDebug struct{}

// NoDebug lets scorers call Debug unconditionally instead of nil-checking.
var NoDebug Debug = noDebug{}

func (noDebug) DebugEnabled() bool { return false }

func (noDebug) Debug(string, ...any) {}
