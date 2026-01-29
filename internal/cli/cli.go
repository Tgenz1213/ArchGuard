package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/git"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

// Execute runs the main application logic, handling path normalization and command routing.
// providerFactory allows injecting a custom LLM provider for testing or specific overrides.
func Execute(providerFactory func(*config.Config) llm.Provider) error {
	fmt.Println("ArchGuard - Architectural Drift Detector")

	repoRoot, err := git.GetRepoRoot()
	if err != nil {
		return fmt.Errorf("%v (ArchGuard must be run inside a git repository)", err)
	}

	cwd, _ := os.Getwd()
	repoRoot = filepath.Clean(repoRoot)
	cwd = filepath.Clean(cwd)

	if !strings.EqualFold(cwd, repoRoot) {
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			if !strings.HasPrefix(arg, "-") {
				absPath := filepath.Join(cwd, arg)
				relPath, err := filepath.Rel(repoRoot, absPath)
				if err == nil {
					relPath = filepath.ToSlash(relPath)
					os.Args[i] = relPath
				}
			}
		}

		if err := os.Chdir(repoRoot); err != nil {
			return fmt.Errorf("error changing to git root: %v", err)
		}
	}

	cfg, err := config.LoadConfig("archguard.yaml")
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	}

	indexFile := ".archguard/index.json"
	if cfg.IndexFile != "" {
		indexFile = cfg.IndexFile
	}

	var provider llm.Provider
	if providerFactory != nil {
		provider = providerFactory(cfg)
	} else {
		switch cfg.LLM.Provider {
		case "openai":
			apiKey := os.Getenv("ARCHGUARD_API_KEY")
			if apiKey == "" {
				fmt.Println("Warning: ARCHGUARD_API_KEY is not set. OpenAI provider may fail.")
			}
			provider = llm.NewOpenAIProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model)
		case "ollama":
			provider = llm.NewOllamaProvider(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.VectorStore.Model, cfg.LLM.Temperature)
		default:
			return fmt.Errorf("unknown provider: %s", cfg.LLM.Provider)
		}
	}

	if len(os.Args) < 2 {
		printUsage()
		return fmt.Errorf("no command provided")
	}

	switch os.Args[1] {
	case "check":
		return runCheck(cfg, provider, indexFile, os.Args[2:])
	case "index":
		return runIndex(cfg, provider, indexFile)
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func runCheck(cfg *config.Config, provider llm.Provider, indexFile string, args []string) error {
	checkFlags := flag.NewFlagSet("check", flag.ExitOnError)
	staged := checkFlags.Bool("staged", false, "Scan staged files only")
	all := checkFlags.Bool("all", false, "Scan all tracked files")
	debug := checkFlags.Bool("debug", false, "Enable debug logging")
	ci := checkFlags.Bool("ci", false, "Enable CI-safe mode (Warn-Open behavior)")

	checkFlags.Parse(args)
	files := checkFlags.Args()

	store := index.NewStore()
	currentHash, err := store.CalculateHash(cfg.Analysis.ADRPath, cfg.VectorStore.Model)
	if err != nil {
		return fmt.Errorf("failed to calculate ADR hash: %v", err)
	}

	if err := store.Load(indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
		return fmt.Errorf("index mismatch or load failed (run 'archguard index' to rebuild): %v", err)
	}

	var contentProvider analysis.ContentProvider
	if len(files) > 0 {
		target := files[0]
		if target == "." {
			contentProvider = &analysis.AllProvider{}
		} else {
			contentProvider = &analysis.SingleFileProvider{Path: target}
		}
	} else if *staged {
		contentProvider = &analysis.StagedProvider{}
	} else if *all {
		contentProvider = &analysis.AllProvider{}
	} else {
		contentProvider = &analysis.UncommittedProvider{}
	}

	if *debug {
		fmt.Println("[DEBUG] Mode Enabled")
	}

	engine := analysis.NewEngine(cfg, store, provider, contentProvider, *debug, *ci)
	if err := engine.Run(context.Background()); err != nil {
		return fmt.Errorf("analysis failed: %v", err)
	}
	fmt.Println("No architectural violations found.")
	return nil
}

func runIndex(cfg *config.Config, provider llm.Provider, indexFile string) error {
	store := index.NewStore()
	if err := store.BuildIndex(context.Background(), cfg.Analysis.ADRPath, cfg.VectorStore.Model, provider, cfg.Analysis.AcceptedStatuses); err != nil {
		return fmt.Errorf("indexing failed: %v", err)
	}
	if err := store.Save(indexFile); err != nil {
		return fmt.Errorf("failed to save index: %v", err)
	}
	fmt.Println("ADR Index updated successfully.")
	return nil
}

func printUsage() {
	fmt.Println("Usage: archguard <command> [arguments]")
	fmt.Println("\nCommands:")
	fmt.Println("  check    Check for architectural violations")
	fmt.Println("  index    Rebuild the ADR index")
}
