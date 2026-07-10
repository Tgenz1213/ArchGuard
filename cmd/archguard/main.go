package main

import (
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
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ArchGuard version %s, commit %s, built at %s\n", version, commit, date)
		os.Exit(0)
	}

	if exitCode, err := cli.Execute(nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(int(exitCode))
	}
	os.Exit(int(cli.ExitSuccess))
}
