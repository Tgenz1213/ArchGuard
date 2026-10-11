package analysis

import "strings"

const fence = "```"

func unwrapMarkdown(quote string) string {
	trimmed := strings.TrimSpace(quote)

	if inner, ok := strings.CutPrefix(trimmed, fence); ok && len(inner) >= len(fence) && strings.HasSuffix(inner, fence) {
		return unwrapFence(strings.TrimSuffix(inner, fence))
	}

	for _, ticks := range []string{"``", "`"} {
		if len(trimmed) > 2*len(ticks) && strings.HasPrefix(trimmed, ticks) && strings.HasSuffix(trimmed, ticks) {
			return strings.TrimSpace(trimmed[len(ticks) : len(trimmed)-len(ticks)])
		}
	}

	return quote
}

// A multi-line fence opens with an info string such as a language tag, which is not code.
func unwrapFence(body string) string {
	_, code, multiLine := strings.Cut(body, "\n")
	if !multiLine {
		return strings.TrimSpace(body)
	}

	return strings.TrimRight(code, "\r\n")
}
