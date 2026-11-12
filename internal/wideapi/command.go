package wideapi

import (
	"context"
	"io"
	"os"

	"github.com/alecthomas/kong"
	"github.com/tgenz1213/archguard/internal/output"
)

const (
	exitError = 1
	exitUsage = 2
)

type commandLine struct {
	Base string `arg:"" optional:"" default:"origin/main" help:"The ref the branch forked from."`
	Head string `arg:"" optional:"" default:"HEAD" help:"The commit being pushed."`
}

func Execute(ctx context.Context) int {
	return Run(ctx, os.Args[1:], os.Stderr)
}

// Run prints what it finds as warnings and exits 0 however much it finds, since it never blocks a push.
func Run(ctx context.Context, args []string, stderr io.Writer) int {
	printer := output.New(stderr, false, output.WithProgress(false))

	var cl commandLine

	exited := -1

	parser, err := kong.New(
		&cl,
		kong.Name("wideapi"),
		kong.Description("Warns when a branch changes the exported API of a package that many others import."),
		kong.Writers(stderr, stderr),
		kong.Exit(func(code int) { exited = code }),
	)
	if err != nil {
		printer.Error("building the command line: %v", err)
		return exitError
	}

	if _, err := parser.Parse(args); exited >= 0 || err != nil {
		if exited < 0 {
			printer.Error("%v", err)
			return exitUsage
		}

		return exited
	}

	result, err := Check(ctx, ".", cl.Base, cl.Head, DefaultMinReach)
	result.Report(printer, DefaultMinReach)

	if err != nil {
		printer.Error("%v", err)
		return exitError
	}

	return 0
}
