package cli

import (
	"bufio"
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

const defaultADRPath = "./docs/arch"
const configFilename = "archguard.yaml"

// Execute parses the command-line arguments, normalizes paths relative to the git root,
// and routes execution to the appropriate command handler.
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

	if len(os.Args) < 2 {
		printUsage()
		return fmt.Errorf("no command provided")
	}

	if os.Args[1] == "init" {
		return runInit()
	}

	cfg, err := config.LoadConfig(configFilename)
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

// runInit initializes a new ArchGuard project by prompting the user for configuration
// preferences and creating the necessary directory structure and config files.
func runInit() error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("Enter ADR directory path [%s]: ", defaultADRPath)
	scanner.Scan()
	if scanner.Err() != nil {
		return fmt.Errorf("input error: %v", scanner.Err())
	}
	adrPath := strings.TrimSpace(scanner.Text())
	if adrPath == "" {
		adrPath = defaultADRPath
	}

	createdDir := false
	if _, err := os.Stat(adrPath); os.IsNotExist(err) {
		fmt.Printf("Directory '%s' does not exist. Create it now? (y/n): ", adrPath)
		scanner.Scan()
		if scanner.Err() != nil {
			return fmt.Errorf("input error: %v", scanner.Err())
		}
		if strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			if err := os.MkdirAll(adrPath, 0755); err != nil {
				return fmt.Errorf("failed to create ADR directory: %v", err)
			}
			fmt.Printf("Created directory: %s\n", adrPath)
			createdDir = true
		} else {
			fmt.Println("Skipping directory creation.")
		}
	}

	if createdDir {
		fmt.Print("Would you like to include a standard ADR_TEMPLATE.md to get started? (y/n): ")
		scanner.Scan()
		if scanner.Err() != nil {
			return fmt.Errorf("input error: %v", scanner.Err())
		}
		if strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			templatePath := filepath.Join(adrPath, "ADR_TEMPLATE.md")
			if err := os.WriteFile(templatePath, []byte(adrTemplateContent), 0644); err != nil {
				return fmt.Errorf("failed to create ADR template: %v", err)
			}
			fmt.Printf("Created template: %s\n", templatePath)
		}
	}

	if _, err := os.Stat(configFilename); err == nil {
		fmt.Printf("%s already exists. Overwrite with defaults? (y/n): ", configFilename)
		scanner.Scan()
		if scanner.Err() != nil {
			return fmt.Errorf("input error: %v", scanner.Err())
		}
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			fmt.Println("Initialization cancelled.")
			return nil
		}
	}

	configContent := generateConfig(adrPath)
	if err := os.WriteFile(configFilename, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to create config file: %v", err)
	}
	fmt.Printf("Created config: %s\n", configFilename)

	if err := os.MkdirAll(".archguard/cache", 0755); err != nil {
		return fmt.Errorf("failed to create .archguard directory: %v", err)
	}
	fmt.Println("Created directory: .archguard/cache")

	if err := ensureGitignore(); err != nil {
		return fmt.Errorf("failed to update .gitignore: %v", err)
	}

	fmt.Println("\nArchGuard initialized successfully!")
	fmt.Println("Next steps:")
	fmt.Println("  1. Add your ADR files to", adrPath)
	fmt.Println("  2. Run: archguard index")
	fmt.Println("  3. Run: archguard check")
	return nil
}

// generateConfig creates the default YAML configuration string based on the provided ADR path.
func generateConfig(adrPath string) string {
	return fmt.Sprintf(`version: "1"

llm:
  provider: "ollama"
  model: "llama3.2"
  base_url: "http://localhost:11434"
  max_tokens: 8000
  temperature: 0.0

vector_store:
  provider: "ollama"
  model: "nomic-embed-text"
  embedding_dim: 768
  similarity_threshold: 0.75

analysis:
  adr_path: "%s"
  accepted_statuses: ["Accepted", "Active"]
  exclude_patterns:
    - "**/*_test.go"
    - "vendor/**"
    - "go.sum"
    - "README.md"
    - "bin/**"
`, adrPath)
}

// ensureGitignore ensures the .archguard/ directory is ignored by git to prevent
// local caches and indexes from being committed.
func ensureGitignore() error {
	const gitignorePath = ".gitignore"
	const archguardEntry = ".archguard/"

	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == archguardEntry {
			return nil
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}()

	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	if _, err := f.WriteString(archguardEntry + "\n"); err != nil {
		return err
	}

	fmt.Printf("Added %s to .gitignore\n", archguardEntry)
	return nil
}

const adrTemplateContent = `---
title: "[Short, Descriptive Title]"
status: "[Accepted | Proposed | Superseded]"
scope: "[Optional: glob pattern, e.g., **/*.go]"
---

# [ADR Title]

## Context

[Describe the problem or context that requires a decision.]

## Decision

[Clearly state the decision and any rules or constraints it imposes.]

## Consequences

[Describe the expected outcomes, both positive and negative.]
`

// runCheck executes the architectural drift analysis against a set of files
// based on the provided flags and ADR index.
func runCheck(cfg *config.Config, provider llm.Provider, indexFile string, args []string) error {
	checkFlags := flag.NewFlagSet("check", flag.ExitOnError)
	staged := checkFlags.Bool("staged", false, "Scan staged files only")
	all := checkFlags.Bool("all", false, "Scan all tracked files")
	debug := checkFlags.Bool("debug", false, "Enable debug logging")
	ci := checkFlags.Bool("ci", false, "Enable CI-safe mode (Warn-Open behavior)")

	if err := checkFlags.Parse(args); err != nil {
		return fmt.Errorf("error parsing flags: %v", err)
	}

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

// runIndex scans the ADR directory and builds a vector index for subsequent drift analysis.
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
	fmt.Println("  init     Initialize ArchGuard in the current repository (local setup)")
	fmt.Println("  check    Check for architectural violations")
	fmt.Println("  index    Rebuild the ADR index")
}
