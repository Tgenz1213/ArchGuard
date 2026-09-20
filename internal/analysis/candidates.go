package analysis

import (
	"fmt"
	"strings"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
)

type candidateSource struct {
	store index.VectorStore
}

func (c candidateSource) For(file, content string, debug stage.Debug) []stage.Candidate {
	// Header only, to keep the scan cheap.
	header := content
	if len(header) > 2000 {
		header = truncateRuneSafe(header, 2000)
	}

	var candidates []stage.Candidate
	for _, r := range c.store.ScopedADRs(file) {
		if strings.Contains(header, fmt.Sprintf("archguard-ignore: %s", r.ADR.ID)) {
			debug.Printf("  Skipping ADR %s (Suppressed)\n", r.ADR.Title)
			continue
		}
		candidates = append(candidates, stage.Candidate{ADR: r.ADR})
	}
	return candidates
}
