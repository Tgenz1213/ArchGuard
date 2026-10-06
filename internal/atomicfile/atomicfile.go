// Package atomicfile writes files all-or-nothing via a temp file and rename.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path via a temp file and rename, so a reader never sees a partial file.
func Write(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup; the write error is what matters
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup; the rename error is what matters
		return err
	}

	return nil
}
