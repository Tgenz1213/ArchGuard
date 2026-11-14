package wideapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReachCountsDirectAndTransitiveImporters(t *testing.T) {
	pkgs := []pkg{
		{ImportPath: "m/base"},
		{ImportPath: "m/mid", Imports: []string{"m/base", "fmt"}},
		{ImportPath: "m/top", Imports: []string{"m/mid"}},
		{ImportPath: "m/other", Imports: []string{"m/base", "m/mid"}},
	}

	assert.Equal(t, map[string]int{"m/base": 3, "m/mid": 2}, reaches(pkgs))
}

func TestReachCountsAnImporterOnceWhateverTheRoute(t *testing.T) {
	pkgs := []pkg{
		{ImportPath: "m/base"},
		{ImportPath: "m/left", Imports: []string{"m/base"}},
		{ImportPath: "m/right", Imports: []string{"m/base"}},
		{ImportPath: "m/top", Imports: []string{"m/left", "m/right", "m/base"}},
	}

	assert.Equal(t, 3, reaches(pkgs)["m/base"])
}
