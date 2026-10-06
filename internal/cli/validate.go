package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
)

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

	if err := checkMappedFields(mappings); err != nil {
		return nil, err
	}

	if err := checkSourceKeyCollisions(mappings); err != nil {
		return nil, err
	}

	return mappings, nil
}

func checkMappedFields(mappings map[string]string) error {
	canonicalFields := make(map[string]bool, len(index.CanonicalFrontMatterFields))
	for _, field := range index.CanonicalFrontMatterFields {
		canonicalFields[field] = true
	}

	for canonical := range mappings {
		if !canonicalFields[canonical] {
			return fmt.Errorf(
				"unknown analysis.frontmatter_mappings field %q: must be one of %s",
				canonical,
				strings.Join(index.CanonicalFrontMatterFields, ", "),
			)
		}
	}

	return nil
}

func checkSourceKeyCollisions(mappings map[string]string) error {
	sourceKeyOwner := make(map[string]string, len(index.CanonicalFrontMatterFields))

	for _, canonical := range index.CanonicalFrontMatterFields {
		sourceKey := canonical
		if mapped, ok := mappings[canonical]; ok && mapped != "" {
			sourceKey = mapped
		}

		if owner, exists := sourceKeyOwner[sourceKey]; exists {
			return collisionError(owner, canonical, sourceKey)
		}

		sourceKeyOwner[sourceKey] = canonical
	}

	return nil
}

func collisionError(owner, canonical, sourceKey string) error {
	err := fmt.Errorf("analysis.frontmatter_mappings collision: %q and %q both resolve to YAML key %q", owner, canonical, sourceKey)

	for _, field := range []string{owner, canonical} {
		if field == sourceKey {
			err = fmt.Errorf("%w; %q reads that key by default, so map %q to another key (e.g. %s: %s_field)", err, field, field, field, field)
		}
	}

	return err
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
