package index

type Summary struct {
	Discovered     int
	Valid          int
	ParseFailed    []string
	StatusRejected int
	DuplicateIDs   map[string][]string // ADR ID -> RelPaths sharing it
	NoScope        []string            // RelPaths of valid ADRs with no scope set
	MalformedRules []MalformedRules
}

func (s Summary) IsEmpty() bool {
	return s.Valid == 0
}

// Runs post-merge so duplicate IDs are caught across providers, not just within one.
func summarizeCorpus(validADRs []ADR, stats FetchStats) Summary {
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

	return Summary{
		Discovered:     stats.Discovered,
		Valid:          len(validADRs),
		ParseFailed:    stats.ParseFailed,
		StatusRejected: stats.StatusRejected,
		DuplicateIDs:   duplicates,
		NoScope:        noScope,
		MalformedRules: stats.MalformedRules,
	}
}
