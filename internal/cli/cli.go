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
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/git"
	"github.com/tgenz1213/archguard/internal/inference"
)

type ExitCode int

const (
	ExitSuccess           ExitCode = 0
	ExitError             ExitCode = 1
	ExitUsage             ExitCode = 2
	ExitConfig            ExitCode = 3
	ExitDriftDetected     ExitCode = 4
	ExitIndexError        ExitCode = 5
	ExitStageUnavailable  ExitCode = 6
	ExitStagePrecondition ExitCode = 7
	// 128 + SIGINT, the shell convention for a run stopped by a signal.
	ExitInterrupted ExitCode = 130
)

var errInterrupted = errors.New("interrupted")

const defaultADRPath = "./docs/arch"
const configFilename = "archguard.yaml"

// Test injection points for Execute; zero value in production.
type ProviderFactories struct {
	Chat  func(*config.Config) inference.Provider
	Embed func(*config.Config) inference.Embedder
}

func Execute(ctx context.Context, factories ProviderFactories) (ExitCode, error) {
	code, err := execute(ctx, factories)
	if err != nil && ctx.Err() != nil {
		return ExitInterrupted, errInterrupted
	}

	return code, err
}

func execute(ctx context.Context, factories ProviderFactories) (ExitCode, error) {
	inv, code, err := parseCommandLine(os.Args[1:], os.Stdout, os.Stderr)
	if inv == nil {
		return code, err
	}

	colors, restoreConsole := decideColors(inv.color())
	defer restoreConsole()

	// check's stdout carries only its result (docs/arch/0028).
	isCheck := inv.command == "check"
	jsonOutput := isCheck && inv.check.jsonOutput()

	const banner = "ArchGuard - Architectural Drift Detector"

	switch {
	case isCheck && !jsonOutput:
		fmt.Fprintln(os.Stderr, banner)
	case !isCheck:
		fmt.Println(banner)
	}

	repoRoot, err := git.GetRepoRoot(ctx)
	if err != nil {
		return ExitError, fmt.Errorf("%w (ArchGuard must be run inside a git repository)", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ExitError, fmt.Errorf("error reading the working directory: %w", err)
	}

	repoRoot = filepath.Clean(repoRoot)
	cwd = filepath.Clean(cwd)

	inv.check.resolvePaths(cwd, repoRoot)

	if !strings.EqualFold(cwd, repoRoot) {
		if err := os.Chdir(repoRoot); err != nil {
			return ExitError, fmt.Errorf("error changing to git root: %w", err)
		}
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		colors.stderrPrinter(false).Warn("failed to load .env: %v", err)
	}

	if inv.command == "init" {
		if err := runInit(); err != nil {
			return ExitError, err
		}

		return ExitSuccess, nil
	}

	cfg, err := config.LoadConfig(configFilename)
	if err != nil {
		return ExitConfig, fmt.Errorf("error loading config: %w", err)
	}

	if cfg.ProjectName == "" {
		cfg.ProjectName = filepath.Base(repoRoot)
	}

	indexFile := ".archguard/index.json"
	if cfg.IndexFile != "" {
		indexFile = cfg.IndexFile
	}

	if err := validateProviderConfig(cfg); err != nil {
		return ExitConfig, err
	}

	adrIDPattern, err := compileADRIDPattern(cfg)
	if err != nil {
		return ExitConfig, err
	}

	frontmatterMappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		return ExitConfig, err
	}

	var (
		chatProvider  inference.Chatter
		embedProvider inference.Embedder
	)

	if factories.Chat != nil {
		chat := factories.Chat(cfg)

		embedProvider, err = resolveEmbedProviderInstance(cfg, chat, factories.Embed)
		if err != nil {
			return ExitConfig, err
		}

		chatProvider = chat
	} else {
		providerWarnings := colors.stdoutPrinter(false)
		if isCheck {
			providerWarnings = colors.stderrPrinter(false)
		}

		chatProvider, embedProvider, err = buildProviders(providerWarnings, cfg, os.Getenv("ARCHGUARD_API_KEY"), os.Getenv("ARCHGUARD_EMBEDDING_API_KEY"))
		if err != nil {
			return ExitConfig, err
		}
	}

	setup := runSetup{
		cfg:                 cfg,
		chat:                chatProvider,
		embed:               embedProvider,
		indexFile:           indexFile,
		adrIDPattern:        adrIDPattern,
		frontmatterMappings: frontmatterMappings,
	}

	if inv.command == "check" {
		return runCheck(ctx, setup, inv.check, colors)
	}

	return runIndexCommand(ctx, setup, colors)
}

type runSetup struct {
	cfg                 *config.Config
	chat                inference.Chatter
	embed               inference.Embedder
	indexFile           string
	adrIDPattern        *regexp.Regexp
	frontmatterMappings map[string]string
}
