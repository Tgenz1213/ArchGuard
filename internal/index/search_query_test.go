package index_test

import (
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/index"
)

func TestSearchQuery_HasNoDistanceThresholdPredicate(t *testing.T) {
	if strings.Contains(index.SearchQuery, "<= $") {
		t.Fatalf("SearchQuery still has a SQL-level distance-threshold predicate; scope/threshold filtering must happen in Go, not SQL -- query:\n%s", index.SearchQuery)
	}
}
