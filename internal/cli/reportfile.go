package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tgenz1213/archguard/internal/atomicfile"
)

func checkReportDestination(path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fmt.Errorf("cannot write the report to %s: it is a directory", path)
	}

	dir := filepath.Dir(path)

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot write the report to %s: %w", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("cannot write the report to %s: %s is not a directory", path, dir)
	}

	return nil
}

func saveReport(path string, data []byte) error {
	if err := checkReportDestination(path); err != nil {
		return err
	}

	if err := atomicfile.Write(path, data); err != nil {
		return fmt.Errorf("failed to write the report to %s: %w", path, err)
	}

	return nil
}
