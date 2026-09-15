package index

// IndexSummary reports an ADR corpus's health: how many were discovered vs.
// usable, and structural problems among the ones that qualified.
type IndexSummary struct {
	Discovered     int
	Valid          int
	ParseFailed    []string
	StatusRejected int
	DuplicateIDs   map[string][]string // ADR ID -> RelPaths sharing it
	NoScope        []string            // RelPaths of valid ADRs with no scope set
}

// IsEmpty reports whether no ADR survived to be indexed.
func (s IndexSummary) IsEmpty() bool {
	return s.Valid == 0
}

// summarizeCorpus computes duplicate-ID and no-scope structural checks once,
// post-merge, so they see collisions across providers, not just within one.
func summarizeCorpus(validADRs []ADR, stats FetchStats) IndexSummary {
	byID := make(map[string][]string)
	var noScope []string
	for _, adr := range validADRs {
		byID[adr.ID] = append(byID[adr.ID], adr.RelPath)
		if len(adr.Scope) == 0 {
			noScope = append(noScope, adr.RelPath)
		}
	}

	duplicates := make(map[string][]string)
	for id, paths := range byID {
		if len(paths) > 1 {
			duplicates[id] = paths
		}
	}

	return IndexSummary{
		Discovered:     stats.Discovered,
		Valid:          len(validADRs),
		ParseFailed:    stats.ParseFailed,
		StatusRejected: stats.StatusRejected,
		DuplicateIDs:   duplicates,
		NoScope:        noScope,
	}
}
