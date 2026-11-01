package output_test

import (
	"slices"
	"testing"

	"github.com/tgenz1213/archguard/internal/output"
)

func TestCoverage_AddScreenedOutCountsFilesAndSortsNodes(t *testing.T) {
	var coverage output.Coverage
	for _, node := range []string{"strict", "", "screen", "strict"} {
		coverage.AddScreenedOut(node)
	}

	if coverage.FilesScreenedOut != 3 || !slices.Equal(coverage.ScreenedOutBy, []string{"screen", "strict"}) {
		t.Errorf("coverage = %+v, want 3 files screened out by screen and strict", coverage)
	}
}
