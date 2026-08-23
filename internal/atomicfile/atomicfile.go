// Package atomicfile provides a shared write-tmp-then-rename helper so
// callers get all-or-nothing file writes without duplicating the pattern.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to path via a temp file plus rename, so a reader never
// observes a partially-written file.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup of a partial write
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup; the rename error is what matters
		return err
	}
	return nil
}
