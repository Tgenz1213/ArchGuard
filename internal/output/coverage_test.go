package output_test

import (
	"encoding/json"
	"slices"
	"strings"
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

func TestCoverage_JSONLeavesOutTheScreeningNodes(t *testing.T) {
	var coverage output.Coverage
	coverage.AddScreenedOut("screen")

	b, err := json.Marshal(coverage)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(b), "screen\"") || strings.Contains(string(b), "ScreenedOutBy") {
		t.Errorf("coverage JSON = %s, want the screening node names left out", b)
	}
}
