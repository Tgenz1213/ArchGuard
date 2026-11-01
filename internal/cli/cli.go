package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/git"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

type ExitCode int

const (
	ExitSuccess          ExitCode = 0
	ExitError            ExitCode = 1
	ExitUsage            ExitCode = 2
	ExitConfig           ExitCode = 3
	ExitDriftDetected    ExitCode = 4
	ExitIndexError       ExitCode = 5
	ExitNodeUnavailable  ExitCode = 6
	ExitNodePrecondition ExitCode = 7
	ExitNothingChecked   ExitCode = 8
	// 128 + SIGINT, the shell convention for a run stopped by a signal.
	ExitInterrupted ExitCode = 130
)

var errInterrupted = errors.New("interrupted")

const (
	defaultADRPath = "./docs/arch"
	configFilename = "archguard.yaml"
)

// Test injection points for Execute; zero value in production.
type ProviderFactories struct {
	Chat     func(*config.Config) inference.Provider
	Embed    func(*config.Config) inference.Embedder
	Jev      func(model string) node.Asker
	Recorder telemetry.Recorder
}

func Execute(ctx context.Context, version string, factories ProviderFactories) (ExitCode, error) {
	code, err := execute(ctx, version, factories)
	if err != nil && ctx.Err() != nil {
		return ExitInterrupted, errInterrupted
	}

	return code, err
}

func execute(ctx context.Context, version string, factories ProviderFactories) (ExitCode, error) {
	inv, code, err := parseCommandLine(os.Args[1:], os.Stdout, os.Stderr, version)
	if inv == nil {
		return code, err
	}

	colors, restoreConsole := decideColors(inv.color())
	defer restoreConsole()

	printBanner(inv)

	repoRoot, err := enterRepoRoot(ctx, inv)
	if err != nil {
		return ExitError, err
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		colors.stderrPrinter(false).Warn("failed to load .env: %v", err)
	}

	if inv.command == "init" {
		if err := runInit(inv.configPath()); err != nil {
			return ExitError, err
		}

		return ExitSuccess, nil
	}

	warnings := colors.stdoutPrinter(false)
	if inv.command == "check" {
		warnings = colors.stderrPrinter(false)
	}

	setup, err := loadSetup(repoRoot, inv.configPath(), warnings, factories)
	if err != nil {
		return ExitConfig, err
	}

	if inv.command == "check" {
		return runCheck(ctx, setup, inv.check, colors)
	}

	return runIndexCommand(ctx, setup, colors)
}

// check's stdout carries only its result (docs/arch/0028).
func printBanner(inv *invocation) {
	const banner = "ArchGuard - Architectural Drift Detector"

	isCheck := inv.command == "check"

	switch {
	case isCheck && !inv.check.jsonOutput():
		fmt.Fprintln(os.Stderr, banner)
	case !isCheck:
		fmt.Println(banner)
	}
}

// Also rewrites inv.check's paths relative to the repo root before the chdir.
func enterRepoRoot(ctx context.Context, inv *invocation) (string, error) {
	repoRoot, err := git.GetRepoRoot(ctx)
	if err != nil {
		return "", fmt.Errorf("%w (ArchGuard must be run inside a git repository)", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("error reading the working directory: %w", err)
	}

	repoRoot = canonicalPath(repoRoot)
	cwd = canonicalPath(cwd)

	inv.check.resolvePaths(cwd, repoRoot)
	inv.index.resolvePaths(cwd)
	inv.init.resolvePaths(cwd)

	if !strings.EqualFold(cwd, repoRoot) {
		if err := os.Chdir(repoRoot); err != nil {
			return "", fmt.Errorf("error changing to git root: %w", err)
		}
	}

	return repoRoot, nil
}

func loadSetup(repoRoot, configPath string, warnings *output.Printer, factories ProviderFactories) (runSetup, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return runSetup{}, fmt.Errorf("error loading config: %w", err)
	}

	if cfg.ProjectName == "" {
		cfg.ProjectName = filepath.Base(repoRoot)
	}

	indexFile := ".archguard/index.json"
	if cfg.IndexFile != "" {
		indexFile = cfg.IndexFile
	}

	if err := validateProviderConfig(cfg); err != nil {
		return runSetup{}, err
	}

	if err := validateCosts(cfg); err != nil {
		return runSetup{}, err
	}

	adrIDPattern, err := compileADRIDPattern(cfg)
	if err != nil {
		return runSetup{}, err
	}

	frontmatterMappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		return runSetup{}, err
	}

	warnIgnoredExcludedHeaders(warnings, cfg.Analysis)

	chat, embed, err := selectProviders(warnings, cfg, factories)
	if err != nil {
		return runSetup{}, err
	}

	return runSetup{
		cfg:                 cfg,
		chat:                chat,
		embed:               embed,
		jev:                 factories.Jev,
		indexFile:           indexFile,
		adrIDPattern:        adrIDPattern,
		frontmatterMappings: frontmatterMappings,
		recorder:            factories.Recorder,
	}, nil
}

type runSetup struct {
	cfg                 *config.Config
	chat                inference.Chatter
	embed               inference.Embedder
	jev                 func(model string) node.Asker
	recorder            telemetry.Recorder
	indexFile           string
	adrIDPattern        *regexp.Regexp
	frontmatterMappings map[string]string
}
