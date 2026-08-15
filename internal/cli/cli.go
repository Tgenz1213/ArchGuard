package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/git"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
)

type ExitCode int

const (
	ExitSuccess       ExitCode = 0
	ExitError         ExitCode = 1
	ExitUsage         ExitCode = 2
	ExitConfig        ExitCode = 3
	ExitDriftDetected ExitCode = 4
	ExitIndexError    ExitCode = 5
)

const defaultADRPath = "./docs/arch"
const configFilename = "archguard.yaml"

// ProviderFactories are test injection points for Execute (zero value in
// production).
type ProviderFactories struct {
	Chat  func(*config.Config) llm.Provider
	Embed func(*config.Config) llm.Provider
}

// Execute parses arguments and runs the requested command.
func Execute(factories ProviderFactories) (ExitCode, error) {
	fmt.Println("ArchGuard - Architectural Drift Detector")

	repoRoot, err := git.GetRepoRoot()
	if err != nil {
		return ExitError, fmt.Errorf("%v (ArchGuard must be run inside a git repository)", err)
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
			return ExitError, fmt.Errorf("error changing to git root: %v", err)
		}
	}

	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to load .env: %v\n", err)
	}

	if len(os.Args) < 2 {
		printUsage()
		return ExitUsage, fmt.Errorf("no command provided")
	}

	command := os.Args[1]
	switch command {
	case "init":
		if err := runInit(); err != nil {
			return ExitError, err
		}
		return ExitSuccess, nil
	case "check", "index":
	default:
		printUsage()
		return ExitUsage, fmt.Errorf("unknown command: %s", command)
	}

	cfg, err := config.LoadConfig(configFilename)
	if err != nil {
		return ExitConfig, fmt.Errorf("error loading config: %v", err)
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

	var chatProvider, embedProvider llm.Provider
	if factories.Chat != nil {
		chatProvider = factories.Chat(cfg)

		embedProvider, err = resolveEmbedProviderInstance(cfg, chatProvider, factories.Embed)
		if err != nil {
			return ExitConfig, err
		}
	} else {
		chatAPIKey := os.Getenv("ARCHGUARD_API_KEY")
		chatProvider, err = buildProvider(cfg.LLM.Provider, chatAPIKey, cfg)
		if err != nil {
			return ExitConfig, err
		}

		embedProviderName, embedAPIKey, reuseChatProvider := resolveEmbedProvider(cfg, chatAPIKey, os.Getenv("ARCHGUARD_EMBEDDING_API_KEY"))
		if reuseChatProvider {
			embedProvider = chatProvider
		} else {
			embedProvider, err = buildProvider(embedProviderName, embedAPIKey, cfg)
			if err != nil {
				return ExitConfig, err
			}
		}
	}

	if command == "check" {
		return runCheck(cfg, chatProvider, embedProvider, indexFile, os.Args[2:])
	}
	return runIndex(context.Background(), cfg, embedProvider, indexFile)
}

// validateProviderConfig checks provider-related config invariants the
// YAML schema itself can't express (see docs/arch/0004-decoupled-chat-and-embedding-providers.md).
func validateProviderConfig(cfg *config.Config) error {
	if cfg.LLM.Provider == "voyage" {
		return fmt.Errorf("llm.provider cannot be \"voyage\": Voyage is an embeddings-only API with no chat capability; use vector_store.provider to configure it for embeddings instead")
	}
	if cfg.LLM.Provider == "claude" && cfg.VectorStore.Provider == "" {
		return fmt.Errorf("vector_store.provider must be set when llm.provider is \"claude\": Claude has no embeddings API, so an embedding-capable provider (openai, ollama, gemini, or voyage) must be chosen explicitly")
	}
	if cfg.VectorStore.Provider == "claude" {
		return fmt.Errorf("vector_store.provider cannot be \"claude\": Claude has no embeddings API; choose an embedding-capable provider (openai, ollama, gemini, or voyage)")
	}
	return nil
}

// resolveEmbedProvider picks the embed provider's name and API key.
// apiKey is embedEnvKey whenever reuse is false -- never chatAPIKey.
func resolveEmbedProvider(cfg *config.Config, chatAPIKey, embedEnvKey string) (name, apiKey string, reuse bool) {
	name = cfg.VectorStore.Provider
	if name == "" {
		name = cfg.LLM.Provider
	}
	if name == cfg.LLM.Provider {
		return name, chatAPIKey, true
	}
	return name, embedEnvKey, false
}

// resolveEmbedProviderInstance is resolveEmbedProvider's mock-injection
// counterpart; errors instead of silently reusing chatProvider when needed.
func resolveEmbedProviderInstance(cfg *config.Config, chatProvider llm.Provider, embedFactory func(*config.Config) llm.Provider) (llm.Provider, error) {
	_, _, reuse := resolveEmbedProvider(cfg, "", "")
	switch {
	case reuse:
		return chatProvider, nil
	case embedFactory != nil:
		return embedFactory(cfg), nil
	default:
		return nil, fmt.Errorf("ProviderFactories.Embed is required: llm.provider and vector_store.provider name different providers")
	}
}

// buildProvider constructs the llm.Provider named by name, using apiKey
// for providers that need one.
func buildProvider(name, apiKey string, cfg *config.Config) (llm.Provider, error) {
	switch name {
	case "openai":
		if apiKey == "" {
			fmt.Printf("Warning: no API key set for %s provider. Requests may fail.\n", name)
		}
		return llm.NewOpenAIProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model), nil
	case "ollama":
		return llm.NewOllamaProvider(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.VectorStore.Model, cfg.LLM.Temperature), nil
	case "gemini":
		if apiKey == "" {
			fmt.Printf("Warning: no API key set for %s provider. Requests may fail.\n", name)
		}
		return llm.NewGeminiProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model), nil
	case "claude":
		if apiKey == "" {
			fmt.Printf("Warning: no API key set for %s provider. Requests may fail.\n", name)
		}
		return llm.NewClaudeProvider(apiKey, cfg.LLM.Model), nil
	case "voyage":
		if apiKey == "" {
			fmt.Printf("Warning: no API key set for %s provider. Requests may fail.\n", name)
		}
		return llm.NewVoyageProvider(apiKey, cfg.VectorStore.Model), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
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
  connection_string: ""
  embedding_concurrency: 5

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
func runCheck(cfg *config.Config, chatProvider, embedProvider llm.Provider, indexFile string, args []string) (ExitCode, error) {
	checkFlags := flag.NewFlagSet("check", flag.ContinueOnError)
	var flagParseOutput bytes.Buffer
	checkFlags.SetOutput(&flagParseOutput)
	staged := checkFlags.Bool("staged", false, "Scan staged files only")
	all := checkFlags.Bool("all", false, "Scan all tracked files")
	debug := checkFlags.Bool("debug", false, "Enable debug logging")
	ci := checkFlags.Bool("ci", false, "Enable CI-safe mode (Warn-Open behavior)")
	updateBaseline := checkFlags.Bool("update-baseline", false, "Scan the full repository and (re)write the baseline file, replacing any existing baseline")

	if err := checkFlags.Parse(args); err != nil {
		if details := strings.TrimSpace(flagParseOutput.String()); details != "" {
			return ExitUsage, fmt.Errorf("error parsing flags: %v\n%s", err, details)
		}
		return ExitUsage, fmt.Errorf("error parsing flags: %v", err)
	}

	files := checkFlags.Args()

	store, err := index.NewVectorStore(cfg)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %v", err)
	}

	var providers []index.Provider
	providers = append(providers, index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses))

	if cfg.Analysis.Confluence.Enabled {
		providers = append(providers, index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		))
	}
	adrProvider := index.NewCompositeProvider(providers...)

	validADRs, err := adrProvider.GetADRs(context.Background())
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to fetch ADRs: %v", err)
	}

	currentHash, err := store.CalculateHash(validADRs, cfg.VectorStore.Model)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to calculate index hash: %v", err)
	}

	if err := store.Load(indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
		fmt.Printf("Index metadata mismatch or missing index. Triggering index rebuild: %v\n", err)
		if _, err := runIndex(context.Background(), cfg, embedProvider, indexFile); err != nil {
			return ExitIndexError, fmt.Errorf("index rebuild failed: %v", err)
		}

		// Reload the index after a successful rebuild to ensure the latest state is in memory.
		currentHash, _ = store.CalculateHash(validADRs, cfg.VectorStore.Model)
		if err := store.Load(indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
			return ExitIndexError, fmt.Errorf("failed to load rebuilt index: %v", err)
		}
	}

	if *updateBaseline && (len(files) > 0 || *staged) {
		fmt.Println("Note: --update-baseline always scans the full repository; ignoring --staged and any file arguments.")
	}
	contentProvider := resolveContentProvider(files, *staged, *all, *updateBaseline)

	if *debug {
		fmt.Println("[DEBUG] Mode Enabled")
	}

	var loadedBaseline *baseline.Baseline
	if !*updateBaseline {
		loadedBaseline, err = baseline.Load(baseline.Path)
		if err != nil {
			return ExitError, fmt.Errorf("failed to load baseline file %s: %v (fix it, or regenerate it with `archguard check --update-baseline`)", baseline.Path, err)
		}
	}

	engine := analysis.NewEngine(cfg, store, chatProvider, contentProvider, *debug, *ci)
	engine.EmbedProvider = embedProvider
	engine.Baseline = loadedBaseline
	engine.UpdateBaseline = *updateBaseline
	if err := engine.Run(context.Background()); err != nil {
		return exitCodeForAnalysisError(err), fmt.Errorf("analysis failed: %v", err)
	}

	if *updateBaseline {
		if err := engine.CollectedBaseline.Save(baseline.Path); err != nil {
			return ExitError, fmt.Errorf("failed to write baseline file %s: %v", baseline.Path, err)
		}
		fmt.Printf("Baseline written to %s (%d violation(s) recorded).\n", baseline.Path, len(engine.CollectedBaseline.Entries))
		return ExitSuccess, nil
	}

	fmt.Println("No new architectural violations found.")
	return ExitSuccess, nil
}

// resolveContentProvider picks the ContentProvider for a check run.
// updateBaseline forces a full-repo scan unconditionally (checked first),
// overriding any file args or --staged/--all, since --update-baseline's
// contract is to snapshot the whole repository regardless of other
// file-selection flags.
func resolveContentProvider(files []string, staged, all, updateBaseline bool) analysis.ContentProvider {
	if updateBaseline {
		return &analysis.AllProvider{}
	}
	if len(files) > 0 {
		if files[0] == "." {
			return &analysis.AllProvider{}
		}
		return &analysis.SingleFileProvider{Path: files[0]}
	}
	if staged {
		return &analysis.StagedProvider{}
	}
	if all {
		return &analysis.AllProvider{}
	}
	return &analysis.UncommittedProvider{}
}

func exitCodeForAnalysisError(err error) ExitCode {
	var driftErr *analysis.DriftDetectedError
	if errors.As(err, &driftErr) {
		return ExitDriftDetected
	}
	return ExitError
}

// runIndex scans the ADR directory and builds a vector index for subsequent drift analysis.
func runIndex(ctx context.Context, cfg *config.Config, embedProvider llm.Provider, indexFile string) (ExitCode, error) {
	store, err := index.NewVectorStore(cfg)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	var providers []index.Provider
	providers = append(providers, index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses))

	if cfg.Analysis.Confluence.Enabled {
		providers = append(providers, index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		))
	}
	adrProvider := index.NewCompositeProvider(providers...)

	if err := store.BuildIndex(ctx, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, embedProvider, adrProvider); err != nil {
		return ExitIndexError, fmt.Errorf("failed to build index: %w", err)
	}

	if err := store.Save(indexFile); err != nil {
		return ExitIndexError, fmt.Errorf("failed to save index: %w", err)
	}
	fmt.Println("ADR Index updated successfully.")
	return ExitSuccess, nil
}

func printUsage() {
	fmt.Println("Usage: archguard <command> [arguments]")
	fmt.Println("\nCommands:")
	fmt.Println("  init     Initialize ArchGuard in the current repository (local setup)")
	fmt.Println("  check    Check for architectural violations")
	fmt.Println("  index    Rebuild the ADR index")
	fmt.Println("\nGlobal Flags:")
	fmt.Println("  -v, --version  Print version information")
}
