package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/tgenz1213/archguard/internal/wideapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := wideapi.Execute(ctx)

	stop()
	os.Exit(code)
}
