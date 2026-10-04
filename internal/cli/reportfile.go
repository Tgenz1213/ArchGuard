package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tgenz1213/archguard/internal/atomicfile"
	"github.com/tgenz1213/archguard/internal/baseline"
)

func checkReportDestination(path string) error {
	if isBaselineFile(path) {
		return fmt.Errorf("cannot write the report to %s: it is the baseline file", path)
	}

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

// Resolved against the working directory, which is the repository root by the time check runs.
func isBaselineFile(path string) bool {
	baselinePath, err := filepath.Abs(baseline.Path)
	if err != nil {
		return false
	}

	target, targetErr := os.Stat(path)
	existing, existingErr := os.Stat(baselinePath)

	if targetErr == nil && existingErr == nil {
		return os.SameFile(target, existing)
	}

	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(path), baselinePath)
	}

	return filepath.Clean(path) == baselinePath
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
