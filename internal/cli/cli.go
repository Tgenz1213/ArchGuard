package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/joho/godotenv"
	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/git"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/llm"
	"github.com/tgenz1213/archguard/internal/output"
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
	Chat  func(*config.Config) llm.Provider
	Embed func(*config.Config) llm.Provider
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

	// Keeps the banner and provider warnings off stdout in JSON mode.
	jsonOutput := inv.command == "check" && inv.check.jsonOutput()
	if !jsonOutput {
		fmt.Println("ArchGuard - Architectural Drift Detector")
	}

	repoRoot, err := git.GetRepoRoot(ctx)
	if err != nil {
		return ExitError, fmt.Errorf("%v (ArchGuard must be run inside a git repository)", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ExitError, fmt.Errorf("error reading the working directory: %w", err)
	}

	repoRoot = filepath.Clean(repoRoot)
	cwd = filepath.Clean(cwd)

	normalizePaths(inv.check.Paths, cwd, repoRoot)

	if !strings.EqualFold(cwd, repoRoot) {
		if err := os.Chdir(repoRoot); err != nil {
			return ExitError, fmt.Errorf("error changing to git root: %v", err)
		}
	}

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		output.New(os.Stderr, false).Warn("failed to load .env: %v", err)
	}

	if inv.command == "init" {
		if err := runInit(); err != nil {
			return ExitError, err
		}

		return ExitSuccess, nil
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

	adrIDPattern, err := compileADRIDPattern(cfg)
	if err != nil {
		return ExitConfig, err
	}

	frontmatterMappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
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
		providerWarnings := output.New(os.Stdout, false)
		if jsonOutput {
			providerWarnings = output.New(os.Stderr, false)
		}

		chatAPIKey := os.Getenv("ARCHGUARD_API_KEY")

		chatProvider, err = buildProvider(providerWarnings, cfg.LLM.Provider, chatAPIKey, cfg)
		if err != nil {
			return ExitConfig, err
		}

		embedProviderName, embedAPIKey, reuseChatProvider := resolveEmbedProvider(cfg, chatAPIKey, os.Getenv("ARCHGUARD_EMBEDDING_API_KEY"))
		if reuseChatProvider {
			embedProvider = chatProvider
		} else {
			embedProvider, err = buildProvider(providerWarnings, embedProviderName, embedAPIKey, cfg)
			if err != nil {
				return ExitConfig, err
			}
		}
	}

	if inv.command == "check" {
		return runCheck(ctx, cfg, chatProvider, embedProvider, indexFile, adrIDPattern, frontmatterMappings, inv.check)
	}

	return runIndexCommand(ctx, cfg, embedProvider, indexFile, adrIDPattern, frontmatterMappings)
}

// Compiled at startup so a bad regex fails as ExitConfig, not per file.
func compileADRIDPattern(cfg *config.Config) (*regexp.Regexp, error) {
	if cfg.Analysis.ADRIDPattern == "" {
		return nil, nil
	}

	re, err := regexp.Compile(cfg.Analysis.ADRIDPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid analysis.adr_id_pattern %q: %w", cfg.Analysis.ADRIDPattern, err)
	}

	return re, nil
}

// Fails fast on a typo'd field or a source-key collision instead of silently misreading ADRs.
func validateFrontmatterMappings(cfg *config.Config) (map[string]string, error) {
	mappings := cfg.Analysis.FrontmatterMappings
	if len(mappings) == 0 {
		return nil, nil
	}

	canonicalFields := make(map[string]bool, len(index.CanonicalFrontMatterFields))
	for _, field := range index.CanonicalFrontMatterFields {
		canonicalFields[field] = true
	}

	for canonical := range mappings {
		if !canonicalFields[canonical] {
			return nil, fmt.Errorf("unknown analysis.frontmatter_mappings field %q: must be one of %s",
				canonical, strings.Join(index.CanonicalFrontMatterFields, ", "))
		}
	}

	sourceKeyOwner := make(map[string]string, len(index.CanonicalFrontMatterFields))
	for _, canonical := range index.CanonicalFrontMatterFields {
		sourceKey := canonical
		if mapped, ok := mappings[canonical]; ok && mapped != "" {
			sourceKey = mapped
		}

		if owner, exists := sourceKeyOwner[sourceKey]; exists {
			err := fmt.Errorf("analysis.frontmatter_mappings collision: %q and %q both resolve to YAML key %q", owner, canonical, sourceKey)

			for _, field := range []string{owner, canonical} {
				if field == sourceKey {
					err = fmt.Errorf("%w; %q reads that key by default, so map %q to another key (e.g. %s: %s_field)", err, field, field, field, field)
				}
			}

			return nil, err
		}

		sourceKeyOwner[sourceKey] = canonical
	}

	return mappings, nil
}

// Invariants the YAML schema can't express (docs/arch/0004).
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

// apiKey is embedEnvKey whenever reuse is false, never chatAPIKey.
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

// Mock-injection counterpart of resolveEmbedProvider; errors rather than silently reusing chatProvider.
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

func buildProvider(warnings *output.Printer, name, apiKey string, cfg *config.Config) (llm.Provider, error) {
	switch name {
	case "openai":
		if apiKey == "" {
			warnings.Warn("no API key set for %s provider. Requests may fail.", name)
		}

		return llm.NewOpenAIProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model), nil
	case "ollama":
		return llm.NewOllamaProvider(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.VectorStore.Model, cfg.LLM.Temperature), nil
	case "gemini":
		if apiKey == "" {
			warnings.Warn("no API key set for %s provider. Requests may fail.", name)
		}

		return llm.NewGeminiProvider(apiKey, cfg.LLM.Model, cfg.VectorStore.Model), nil
	case "claude":
		if apiKey == "" {
			warnings.Warn("no API key set for %s provider. Requests may fail.", name)
		}

		return llm.NewClaudeProvider(apiKey, cfg.LLM.Model), nil
	case "voyage":
		if apiKey == "" {
			warnings.Warn("no API key set for %s provider. Requests may fail.", name)
		}

		return llm.NewVoyageProvider(apiKey, cfg.VectorStore.Model), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

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
  similarity_threshold: 0.75 # Global default; an ADR's own frontmatter similarity_threshold overrides this per-ADR
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
scope: "[Optional: glob pattern, e.g., **/*.go -- or a YAML list of globs, matched with OR semantics]"
---

# [ADR Title]

## Context

[Describe the problem or context that requires a decision.]

## Decision

[Clearly state the decision and any rules or constraints it imposes.]

## Consequences

[Describe the expected outcomes, both positive and negative.]
`

func runCheck(ctx context.Context, cfg *config.Config, chatProvider, embedProvider llm.Provider, indexFile string, adrIDPattern *regexp.Regexp, frontmatterMappings map[string]string, opts checkCmd) (code ExitCode, err error) {
	files := opts.Paths

	jsonOutput := opts.jsonOutput()
	// stderr in JSON mode keeps stdout carrying only the JSON document (docs/arch/0014).
	human := io.Writer(os.Stdout)
	if jsonOutput {
		human = os.Stderr
	}

	out := output.New(human, opts.Debug)

	defer func() {
		if werr := out.Err(); werr != nil && !jsonOutput && code != ExitInterrupted {
			code, err = ExitError, errors.Join(outputWriteError(werr), err)
		}
	}()

	if opts.Format == "json" && opts.UpdateBaseline {
		out.Note("--format json has no effect with --update-baseline; ignoring it.")
	}

	store, err := index.NewVectorStore(cfg, out)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %v", err)
	}

	// Held until we know whether a rebuild will fetch the ADRs again and repeat these warnings.
	fetchOut := out.Group("")

	localProvider := index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses)
	localProvider.SetIDPattern(adrIDPattern)
	localProvider.SetFrontmatterMappings(frontmatterMappings)
	localProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
	localProvider.SetPrinter(fetchOut)
	var providers []index.Provider
	providers = append(providers, localProvider)

	if cfg.Analysis.Confluence.Enabled {
		confluenceProvider := index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		)
		confluenceProvider.SetFrontmatterMappings(frontmatterMappings)
		confluenceProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
		confluenceProvider.SetPrinter(fetchOut)
		providers = append(providers, confluenceProvider)
	}

	adrProvider := index.NewCompositeProvider(providers...)
	adrProvider.SetPrinter(fetchOut)

	validADRs, _, err := adrProvider.GetADRs(ctx)
	if err != nil {
		fetchOut.Flush()
		return ExitIndexError, fmt.Errorf("failed to fetch ADRs: %v", err)
	}

	currentHash, err := store.CalculateHash(validADRs, cfg.VectorStore.Model)
	if err != nil {
		fetchOut.Flush()
		return ExitIndexError, fmt.Errorf("failed to calculate index hash: %v", err)
	}

	if err := store.Load(indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err == nil {
		fetchOut.Flush()
	} else {
		out.Info("Index metadata mismatch or missing index. Triggering index rebuild: %v", err)

		if _, err := runIndex(ctx, cfg, embedProvider, indexFile, adrIDPattern, frontmatterMappings, out); err != nil {
			return ExitIndexError, fmt.Errorf("index rebuild failed: %v", err)
		}

		currentHash, err = store.CalculateHash(validADRs, cfg.VectorStore.Model)
		if err != nil {
			return ExitIndexError, fmt.Errorf("failed to calculate rebuilt index hash: %v", err)
		}

		if err := store.Load(indexFile, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, currentHash); err != nil {
			return ExitIndexError, fmt.Errorf("failed to load rebuilt index: %v", err)
		}
	}

	if opts.UpdateBaseline && (len(files) > 0 || opts.Staged) {
		out.Note("--update-baseline always scans the full repository; ignoring --staged and any file arguments.")
	}

	if opts.BaselineReason != "" && !opts.UpdateBaseline {
		out.Note("--baseline-reason has no effect without --update-baseline; ignoring it.")
	}

	contentProvider := resolveContentProvider(out, files, opts.Staged, opts.All, opts.UpdateBaseline)

	out.Debug("Mode Enabled")

	var loadedBaseline *baseline.Baseline

	loadedBaseline, err = baseline.Load(baseline.Path)
	if err != nil {
		if !opts.UpdateBaseline {
			return ExitError, fmt.Errorf("failed to load baseline file %s: %v (fix it, or regenerate it with `archguard check --update-baseline`)", baseline.Path, err)
		}

		out.Warn("failed to load existing baseline file %s (baseline reasons will not carry forward): %v", baseline.Path, err)
	}

	engine := analysis.NewEngine(cfg, store, chatProvider, contentProvider, opts.Debug, opts.CI)
	engine.EmbedProvider = embedProvider

	analysisCache, cacheErr := cache.NewCache(".")
	if cacheErr != nil {
		out.Warn("analysis cache disabled: %v", cacheErr)
	}

	engine.Cache = analysisCache

	engine.Stages = analysis.BuildStages(cfg, store, embedProvider, out)
	engine.Baseline = loadedBaseline
	engine.UpdateBaseline = opts.UpdateBaseline
	engine.BaselineReason = opts.BaselineReason
	engine.JSONOutput = jsonOutput
	engine.Out = out
	engine.SuggestFixes = opts.SuggestFixes

	runErr := engine.Run(ctx)
	if runErr != nil && ctx.Err() != nil {
		return ExitInterrupted, errInterrupted
	}

	stageFailureCode, stageFailureErr := stageFailureExit(engine.StageFailures)

	if opts.UpdateBaseline {
		if runErr != nil {
			return exitCodeForAnalysisError(runErr), fmt.Errorf("analysis failed: %v", runErr)
		}

		if stageFailureErr != nil {
			return stageFailureCode, fmt.Errorf("%v; baseline not written", stageFailureErr)
		}

		if err := engine.CollectedBaseline.Save(baseline.Path); err != nil {
			return ExitError, fmt.Errorf("failed to write baseline file %s: %v", baseline.Path, err)
		}

		out.Result("Baseline scan complete: %d violation(s) recorded, %d file(s) skipped due to errors, %d ADR check(s) skipped due to LLM errors.", len(engine.CollectedBaseline.Entries), engine.SkippedFiles, engine.SkippedADRChecks)
		out.Result("Baseline written to %s (%d violation(s) recorded).", baseline.Path, len(engine.CollectedBaseline.Entries))
		return ExitSuccess, nil
	}

	var analysisErr error
	if runErr != nil {
		analysisErr = fmt.Errorf("analysis failed: %v", runErr)
	}

	if jsonOutput {
		if err := writeCheckReport(os.Stdout, engine.CollectedViolations, engine.CollectedStages, engine.StageFailures); err != nil {
			return ExitError, errors.Join(fmt.Errorf("failed to write json report: %w", err), stageFailureErr, analysisErr)
		}
	}

	if stageFailureErr != nil {
		return stageFailureCode, stageFailureErr
	}

	if runErr != nil {
		return exitCodeForAnalysisError(runErr), analysisErr
	}

	// Reached only without drift, in either format.
	switch {
	case engine.SkippedADRChecks > 0 && engine.SkippedFiles > 0:
		out.Result("Check completed, but %d ADR check(s) were skipped due to LLM errors and %d file(s) were skipped due to file-context/embedding errors; compliance was not fully verified.", engine.SkippedADRChecks, engine.SkippedFiles)
	case engine.SkippedADRChecks > 0:
		out.Result("Check completed, but %d ADR check(s) were skipped due to LLM errors; compliance was not fully verified.", engine.SkippedADRChecks)
	case engine.SkippedFiles > 0:
		out.Result("Check completed, but %d file(s) were skipped due to file-context/embedding errors; compliance was not fully verified.", engine.SkippedFiles)
	default:
		out.Result("No new architectural violations found.")
	}

	return ExitSuccess, nil
}

type checkReport struct {
	Violations []analysis.Violation    `json:"violations"`
	Count      int                     `json:"count"`
	Stages     []stage.Stats           `json:"stages"`
	Failures   []analysis.StageFailure `json:"failures,omitempty"`
}

func writeCheckReport(w io.Writer, violations []analysis.Violation, stages []stage.Stats, failures []analysis.StageFailure) error {
	if violations == nil {
		violations = []analysis.Violation{}
	}

	if stages == nil {
		stages = []stage.Stats{}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(checkReport{Violations: violations, Count: len(violations), Stages: stages, Failures: failures})
}

// A precondition failure outranks an unavailable dependency when a run has both.
func stageFailureExit(failures []analysis.StageFailure) (ExitCode, error) {
	if len(failures) == 0 {
		return ExitSuccess, nil
	}

	code := ExitStageUnavailable
	for _, f := range failures {
		if f.Kind == stage.KindPreconditionNotMet {
			code = ExitStagePrecondition
		}
	}

	return code, fmt.Errorf("%d stage failure(s) with on_error: fail; compliance was not verified", len(failures))
}

func resolveContentProvider(out *output.Printer, files []string, staged, all, updateBaseline bool) analysis.ContentProvider {
	if updateBaseline {
		return &analysis.AllProvider{}
	}

	if len(files) > 0 {
		if slices.Contains(files, ".") {
			var extras []string
			for _, f := range files {
				if f != "." {
					extras = append(extras, f)
				}
			}

			if len(extras) > 0 {
				out.Note("\".\" scans the whole repository; ignoring extra path argument(s): %v", extras)
			}

			return &analysis.AllProvider{}
		}

		return &analysis.MultiFileProvider{Paths: files}
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

// Separate from runIndex, which check's auto-rebuild calls with its own printer.
func runIndexCommand(ctx context.Context, cfg *config.Config, embedProvider llm.Provider, indexFile string, adrIDPattern *regexp.Regexp, frontmatterMappings map[string]string) (ExitCode, error) {
	out := output.New(os.Stdout, false)

	code, err := runIndex(ctx, cfg, embedProvider, indexFile, adrIDPattern, frontmatterMappings, out)
	if werr := out.Err(); werr != nil && code != ExitInterrupted {
		return ExitError, errors.Join(outputWriteError(werr), err)
	}

	return code, err
}

func outputWriteError(err error) error {
	return fmt.Errorf("failed to write output: %w", err)
}

func runIndex(ctx context.Context, cfg *config.Config, embedProvider llm.Provider, indexFile string, adrIDPattern *regexp.Regexp, frontmatterMappings map[string]string, out *output.Printer) (ExitCode, error) {
	store, err := index.NewVectorStore(cfg, out)
	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	localProvider := index.NewLocalProvider(cfg.Analysis.ADRPath, cfg.Analysis.AcceptedStatuses)
	localProvider.SetIDPattern(adrIDPattern)
	localProvider.SetFrontmatterMappings(frontmatterMappings)
	localProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
	localProvider.SetPrinter(out)
	var providers []index.Provider
	providers = append(providers, localProvider)

	if cfg.Analysis.Confluence.Enabled {
		confluenceProvider := index.NewConfluenceProvider(
			cfg.Analysis.Confluence.Domain,
			cfg.Analysis.Confluence.SpaceID,
			cfg.Analysis.Confluence.Username,
			cfg.Analysis.Confluence.Token,
			cfg.Analysis.AcceptedStatuses,
		)
		confluenceProvider.SetFrontmatterMappings(frontmatterMappings)
		confluenceProvider.SetRulesHeading(cfg.Analysis.RulesHeading)
		confluenceProvider.SetPrinter(out)
		providers = append(providers, confluenceProvider)
	}

	adrProvider := index.NewCompositeProvider(providers...)
	adrProvider.SetPrinter(out)

	result, err := store.BuildIndex(ctx, cfg.VectorStore.Model, cfg.VectorStore.EmbeddingDim, embedProvider, adrProvider)
	if result.Attempted {
		printIndexSummary(result, out)
	}

	if err != nil {
		return ExitIndexError, fmt.Errorf("failed to build index: %w", err)
	}

	// Checked before Save so a failed rebuild leaves the prior index intact.
	if result.IsEmpty() {
		return ExitIndexError, fmt.Errorf("no valid ADRs found among %d discovered; index not updated", result.Discovered)
	}

	if err := store.Save(indexFile); err != nil {
		return ExitIndexError, fmt.Errorf("failed to save index: %w", err)
	}

	return ExitSuccess, nil
}

func printIndexSummary(result index.BuildIndexResult, out *output.Printer) {
	section := out.Indented()
	item := section.Indented()

	out.Result("ADR Index: %d discovered, %d valid.", result.Discovered, result.Valid)

	if len(result.ParseFailed) > 0 {
		section.Result("Skipped (parse failure): %d", len(result.ParseFailed))
		for _, path := range result.ParseFailed {
			item.Result("- %s", path)
		}
	}

	if result.StatusRejected > 0 {
		section.Result("Skipped (status not accepted): %d", result.StatusRejected)
	}

	if len(result.Skipped) > 0 {
		section.Result("Failed to embed or persist: %d", len(result.Skipped))
		for _, skipped := range result.Skipped {
			item.Result("- %s: %v", skipped.RelPath, skipped.Err)
		}
	}

	if len(result.DuplicateIDs) > 0 {
		ids := make([]string, 0, len(result.DuplicateIDs))
		for id := range result.DuplicateIDs {
			ids = append(ids, id)
		}

		sort.Strings(ids)
		section.Result("Duplicate ADR IDs: %d", len(ids))
		for _, id := range ids {
			item.Result("- %q used by: %s", id, strings.Join(result.DuplicateIDs[id], ", "))
		}
	}

	if len(result.NoScope) > 0 {
		section.Result("No scope set (applies to every file): %d", len(result.NoScope))
		for _, path := range result.NoScope {
			item.Result("- %s", path)
		}
	}

	if len(result.MalformedRules) > 0 {
		section.Result("Rules ignored (malformed): %d", len(result.MalformedRules))
		for _, m := range result.MalformedRules {
			item.Result("- %s: %s", m.RelPath, m.Reason)
		}
	}
}
