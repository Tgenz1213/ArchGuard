package main

import (
	"fmt"
	"os"

	"github.com/tgenz1213/archguard/internal/cli"
)

func main() {
	if exitCode, err := cli.Execute(nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(int(exitCode))
	}
	os.Exit(int(cli.ExitSuccess))
}
