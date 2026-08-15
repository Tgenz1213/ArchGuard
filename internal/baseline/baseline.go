package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const Path = "archguard-baseline.json"

type Entry struct {
	ADRID      string `json:"adr_id"`
	File       string `json:"file"`
	QuotedCode string `json:"quoted_code"`
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
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	if b.Entries == nil {
		b.Entries = []Entry{}
	}

	return &b, nil
}

func (b *Baseline) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Sort by (File, ADRID) -- a unique key by construction (Add's overwrite
	// semantics guarantee no duplicate pairs) -- so two --update-baseline
	// runs over an unchanged repo produce byte-identical, diff-free JSON.
	sort.Slice(b.Entries, func(i, j int) bool {
		if b.Entries[i].File != b.Entries[j].File {
			return b.Entries[i].File < b.Entries[j].File
		}
		return b.Entries[i].ADRID < b.Entries[j].ADRID
	})

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

func (b *Baseline) Add(adrID, file, quotedCode string) {
	if b == nil {
		return
	}

	for i, entry := range b.Entries {
		if entry.ADRID == adrID && entry.File == file {
			b.Entries[i] = Entry{
				ADRID:      adrID,
				File:       file,
				QuotedCode: quotedCode,
			}
			return
		}
	}

	b.Entries = append(b.Entries, Entry{
		ADRID:      adrID,
		File:       file,
		QuotedCode: quotedCode,
	})
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
