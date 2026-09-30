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
	cli.Version = fmt.Sprintf("%s, commit %s, built at %s", version, commit, date)

	ctx, stop := cli.NotifyContext(context.Background())

	exitCode, err := cli.Execute(ctx, cli.ProviderFactories{})

	stop()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	os.Exit(int(exitCode))
}
