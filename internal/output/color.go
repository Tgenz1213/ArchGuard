package output

import "github.com/muesli/termenv"

type Option func(*sink)

func WithColor(on bool) Option {
	return func(s *sink) { s.color = on }
}

type style func(termenv.Style) termenv.Style

func warnStyle(s termenv.Style) termenv.Style { return s.Foreground(termenv.ANSIYellow) }

func errorStyle(s termenv.Style) termenv.Style { return s.Foreground(termenv.ANSIRed) }

func debugStyle(s termenv.Style) termenv.Style { return s.Foreground(termenv.ANSIBrightBlack) }

func fileStyle(s termenv.Style) termenv.Style { return s.Foreground(termenv.ANSICyan).Bold() }

func violationStyle(s termenv.Style) termenv.Style { return s.Foreground(termenv.ANSIRed).Bold() }

func baselinedStyle(s termenv.Style) termenv.Style { return s.Faint() }

func (s *sink) paint(st style, text string) string {
	if !s.color || st == nil || text == "" {
		return text
	}

	return st(termenv.ANSI.String(text)).String()
}
