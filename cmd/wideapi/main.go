package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/tgenz1213/archguard/internal/wideapi"
)

func main() {
	base, head := "origin/main", "HEAD"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}

	if len(os.Args) > 2 {
		head = os.Args[2]
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	result, err := wideapi.Check(ctx, ".", base, head, wideapi.DefaultMinReach)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wideapi: %v\n", err)
	}

	fmt.Fprint(os.Stderr, result.String(wideapi.DefaultMinReach))
}
