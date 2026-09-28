//go:build !windows

package cli

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestNotifyContext_SIGTERMCancels(t *testing.T) {
	ctx, stop := NotifyContext(t.Context())
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context not cancelled 5s after SIGTERM")
	}
}
