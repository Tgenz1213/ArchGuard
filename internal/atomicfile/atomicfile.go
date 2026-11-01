// Package atomicfile writes files all-or-nothing via a temp file and rename.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path via a temp file and rename, so a reader never sees a partial file.
func Write(path string, content []byte) error {
	return WriteMode(path, content, 0644)
}

// WriteMode is Write with the file's permission bits set to perm, whatever the umask.
func WriteMode(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := writeTemp(tmpPath, content, perm); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup; the write error is what matters
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup; the rename error is what matters
		return err
	}

	return nil
}

func writeTemp(tmpPath string, content []byte, perm os.FileMode) error {
	if err := os.WriteFile(tmpPath, content, perm); err != nil {
		return err
	}

	return os.Chmod(tmpPath, perm)
}
