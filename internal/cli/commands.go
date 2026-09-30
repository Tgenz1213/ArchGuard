package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/alecthomas/kong"
)

// Version is set by cmd/archguard from build-time ldflags.
var Version = "dev"

type commandLine struct {
	Version versionFlag `short:"v" help:"Print version information."`

	Init  struct{} `cmd:"" help:"Initialize ArchGuard in the current repository (local setup)."`
	Check checkCmd `cmd:"" help:"Check for architectural violations."`
	Index struct{} `cmd:"" help:"Rebuild the ADR index from the configured ADR source(s)."`
}

type checkCmd struct {
	Staged         bool     `help:"Scan staged files only."`
	All            bool     `help:"Scan all tracked files."`
	Debug          bool     `help:"Enable debug logging."`
	CI             bool     `name:"ci" help:"Enable CI-safe mode (Warn-Open behavior)."`
	UpdateBaseline bool     `help:"Scan the full repository and (re)write the baseline file, replacing any existing baseline."`
	BaselineReason string   `placeholder:"TEXT" help:"Reason recorded on every entry --update-baseline writes (e.g. \"accepted-debt\"), overwriting reasons carried forward from the previous baseline. When omitted, a re-run keeps each matching (ADR ID, file) entry's existing reason."`
	Format         string   `enum:"text,json" default:"text" help:"Output format: text or json."`
	SuggestFixes   bool     `help:"Add a short, unverified LLM-suggested fix to each new violation (one extra LLM call per violation)."`
	Paths          []string `arg:"" optional:"" name:"path" help:"Files to check; \".\" scans the whole repository. Defaults to uncommitted changes."`
}

// --update-baseline prints a maintenance summary, not a violation report, so it ignores --format.
func (c checkCmd) jsonOutput() bool {
	return c.Format == "json" && !c.UpdateBaseline
}

type versionFlag bool

func (versionFlag) BeforeReset(app *kong.Kong) error {
	if _, err := fmt.Fprintf(app.Stdout, "ArchGuard version %s\n", Version); err != nil {
		return err
	}

	app.Exit(int(ExitSuccess))

	return nil
}

type invocation struct {
	command string
	check   checkCmd
}

// A nil invocation means the command line was fully handled here: help, version, or a usage error.
func parseCommandLine(args []string, stdout, stderr io.Writer) (*invocation, ExitCode, error) {
	var cl commandLine

	w := &writeRecorder{w: stdout}
	exited := -1

	parser, err := kong.New(&cl,
		kong.Name("archguard"),
		kong.Description("Checks changed code against the rules in your Architectural Decision Records."),
		kong.Writers(w, stderr),
		kong.Exit(func(code int) { exited = code }),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	if err != nil {
		return nil, ExitError, fmt.Errorf("building the command line: %w", err)
	}

	if len(args) > 0 && args[0] == "help" {
		args = append([]string{"--help"}, args[1:]...)
	}

	kctx, err := parser.Parse(args)
	if err != nil && w.err == nil && exited < 0 {
		var parseErr *kong.ParseError
		if errors.As(err, &parseErr) && parseErr.Context != nil {
			// Usage after a mistake is a diagnostic; only requested help is primary output.
			parseErr.Context.Stdout = stderr
			_ = parseErr.Context.PrintUsage(false) //nolint:errcheck // best-effort; the parse error is what's reported
		}
	}

	switch {
	case w.err != nil:
		return nil, ExitError, outputWriteError(w.err)
	case exited >= 0:
		return nil, ExitCode(exited), nil
	case err != nil:
		return nil, ExitUsage, err
	}

	return &invocation{command: strings.Fields(kctx.Command())[0], check: cl.Check}, ExitSuccess, nil
}

type writeRecorder struct {
	w   io.Writer
	err error
}

func (r *writeRecorder) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if err != nil && r.err == nil {
		r.err = err
	}

	return n, err
}

// Relative to the repo root with forward slashes, matching git's paths and baseline entries.
func normalizePaths(paths []string, cwd, repoRoot string) {
	for i, path := range paths {
		if path == "" {
			continue
		}

		target := path
		if !filepath.IsAbs(path) {
			target = filepath.Join(cwd, path)
		}

		if rel, err := filepath.Rel(repoRoot, target); err == nil {
			paths[i] = filepath.ToSlash(rel)
		}
	}
}
