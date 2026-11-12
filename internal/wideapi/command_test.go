package wideapi

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func widenCore(t *testing.T, dir string) {
	t.Helper()

	onBranch(t, dir, func(t *testing.T, dir string) {
		write(t, dir, "core/core.go", "package core\n\nfunc F(a int) int { return a }\n\nfunc G() {}\n")
	})
}

func TestRunWarnsAboutAWideChangeAndStillExitsZero(t *testing.T) {
	dir := newRepo(t)
	widenCore(t, dir)
	t.Chdir(dir)

	var stderr bytes.Buffer

	code := Run(t.Context(), []string{"main", "feature"}, &stderr)

	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "core (reach 5): +func G")
}

func TestRunIsSilentWhenNoWidePackageChanged(t *testing.T) {
	dir := newRepo(t)
	onBranch(t, dir, func(t *testing.T, dir string) { write(t, dir, "p1/own.go", "package p1\n") })
	t.Chdir(dir)

	var stderr bytes.Buffer

	assert.Equal(t, 0, Run(t.Context(), []string{"main", "feature"}, &stderr))
	assert.Empty(t, stderr.String())
}

func TestRunFailsForARefThatDoesNotExist(t *testing.T) {
	dir := newRepo(t)
	t.Chdir(dir)

	var stderr bytes.Buffer

	assert.Equal(t, exitError, Run(t.Context(), []string{"main", "nope"}, &stderr))
	assert.Contains(t, stderr.String(), "Error:")
}

func TestRunRejectsAnUnknownFlagWithTheUsageExitCode(t *testing.T) {
	var stderr bytes.Buffer

	assert.Equal(t, exitUsage, Run(t.Context(), []string{"--nope"}, &stderr))
	assert.Contains(t, stderr.String(), "unknown flag")
}

func TestRunPrintsHelpWithoutChecking(t *testing.T) {
	var stderr bytes.Buffer

	assert.Equal(t, 0, Run(t.Context(), []string{"--help"}, &stderr))
	assert.Contains(t, stderr.String(), "Usage: wideapi")
}
