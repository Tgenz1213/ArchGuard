package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/cli"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestPrintVersion(t *testing.T) {
	var buf bytes.Buffer

	if code := printVersion(&buf); code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}

	if !strings.HasPrefix(buf.String(), "ArchGuard version dev, commit none") {
		t.Errorf("got %q", buf.String())
	}
}

func TestPrintVersionWriteFailureExitsOne(t *testing.T) {
	if code := printVersion(failingWriter{}); code != cli.ExitError {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitError)
	}
}
