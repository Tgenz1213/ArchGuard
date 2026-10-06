package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tgenz1213/archguard/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := cli.NotifyContext(context.Background())

	exitCode, err := cli.Execute(ctx, fmt.Sprintf("%s, commit %s, built at %s", version, commit, date), cli.ProviderFactories{})

	stop()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	os.Exit(int(exitCode))
}
