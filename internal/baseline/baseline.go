package baseline

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/tgenz1213/archguard/internal/atomicfile"
)

const Path = "archguard-baseline.json"

type Entry struct {
	ADRID      string `json:"adr_id"`
	File       string `json:"file"`
	QuotedCode string `json:"quoted_code"`
	// Informational only; IsSuppressed ignores it.
	Reason string `json:"reason,omitempty"`
}

type Baseline struct {
	Entries []Entry `json:"entries"`
}

func New() *Baseline {
	return &Baseline{
		Entries: []Entry{},
	}
}

func Load(path string) (*Baseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}

	if b.Entries == nil {
		b.Entries = []Entry{}
	}

	return &b, nil
}

func (b *Baseline) Save(path string) error {
	if b == nil {
		return nil
	}

	// Sorted so two --update-baseline runs over an unchanged repo produce
	// byte-identical, diff-free JSON.
	sort.Slice(b.Entries, func(i, j int) bool {
		if b.Entries[i].File != b.Entries[j].File {
			return b.Entries[i].File < b.Entries[j].File
		}

		return b.Entries[i].ADRID < b.Entries[j].ADRID
	})

	encoded, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	return atomicfile.Write(path, encoded)
}

func (b *Baseline) Add(entry Entry) {
	if b == nil {
		return
	}

	for i, existing := range b.Entries {
		if existing.ADRID == entry.ADRID && existing.File == entry.File {
			b.Entries[i] = entry
			return
		}
	}

	b.Entries = append(b.Entries, entry)
}

func (b *Baseline) ReasonFor(adrID, file string) string {
	if b == nil {
		return ""
	}

	for _, entry := range b.Entries {
		if entry.ADRID == adrID && entry.File == file {
			return entry.Reason
		}
	}

	return ""
}

func (b *Baseline) IsSuppressed(adrID, file, currentFileContent string) bool {
	if b == nil {
		return false
	}

	for _, entry := range b.Entries {
		if entry.ADRID == adrID && entry.File == file {
			if entry.QuotedCode == "" {
				return true
			}

			return strings.Contains(currentFileContent, entry.QuotedCode)
		}
	}

	return false
}
