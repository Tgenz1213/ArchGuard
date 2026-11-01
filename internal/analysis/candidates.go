package analysis

import (
	"fmt"
	"strings"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/index"
)

type candidateSource struct {
	store index.VectorStore
}

func (c candidateSource) For(file, content string, debug node.Debug) ([]node.Candidate, error) {
	// Header only, to keep the scan cheap.
	header := content
	if len(header) > 2000 {
		header = truncateRuneSafe(header, 2000)
	}

	scoped, err := c.store.ScopedADRs(file)
	if err != nil {
		return nil, err
	}

	var candidates []node.Candidate
	for _, r := range scoped {
		if strings.Contains(header, fmt.Sprintf("archguard-ignore: %s", r.ADR.ID)) {
			debug.Debug("Skipping ADR %s (Suppressed)", r.ADR.Title)
			continue
		}

		candidates = append(candidates, node.Candidate{ADR: r.ADR})
	}

	return candidates, nil
}
