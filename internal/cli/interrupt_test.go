package cli

import (
	"context"
	"os"
	"testing"
)

func TestExecute_ErrorWhileCancelledExitsInterrupted(t *testing.T) {
	origArgs := os.Args

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		os.Args = origArgs

		if err := os.Chdir(origWd); err != nil {
			t.Errorf("restoring working directory: %v", err)
		}
	}()

	setupExecuteTestRepo(t)

	os.Args = []string{"archguard", "index"}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var (
		code    ExitCode
		execErr error
	)

	captureStdout(t, func() {
		code, execErr = Execute(ctx, ProviderFactories{})
	})

	if code != ExitInterrupted {
		t.Fatalf("exit code = %d, want %d (err: %v)", code, ExitInterrupted, execErr)
	}

	if execErr == nil || execErr.Error() != "interrupted" {
		t.Errorf("err = %v, want \"interrupted\"", execErr)
	}
}

func TestExecute_NilErrorIsNotRemappedWhenCancelled(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"archguard", "--help"}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var code ExitCode

	captureStdout(t, func() {
		var err error

		code, err = Execute(ctx, ProviderFactories{})
		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}
	})

	if code != ExitSuccess {
		t.Fatalf("exit code = %d, want %d: a command that returned no error must keep its exit code", code, ExitSuccess)
	}
}
