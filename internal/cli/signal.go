package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// NotifyContext cancels on SIGINT or SIGTERM; once cancelled, a second signal kills the process as usual.
func NotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ctx.Done()
		stop()
	}()

	return ctx, stop
}
