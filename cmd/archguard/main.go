package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/tgenz1213/archguard/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		os.Exit(int(printVersion(os.Stdout)))
	}

	ctx, stop := cli.NotifyContext(context.Background())

	exitCode, err := cli.Execute(ctx, cli.ProviderFactories{})

	stop()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(int(exitCode))
	}

	os.Exit(int(cli.ExitSuccess))
}

func printVersion(w io.Writer) cli.ExitCode {
	if _, err := fmt.Fprintf(w, "ArchGuard version %s, commit %s, built at %s\n", version, commit, date); err != nil {
		return cli.ExitError
	}

	return cli.ExitSuccess
}
