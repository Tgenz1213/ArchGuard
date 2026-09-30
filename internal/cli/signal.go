package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// NotifyContext cancels on SIGINT or SIGTERM; once cancelled, a second signal kills the process as usual.
func NotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	// A broken stdout or stderr pipe then returns EPIPE for the write-failure rules instead of killing the process.
	signal.Ignore(syscall.SIGPIPE)

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ctx.Done()
		stop()
	}()

	return ctx, stop
}
