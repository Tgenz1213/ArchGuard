package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestValidateProviderConfig(t *testing.T) {
	tests := []struct {
		name          string
		llmProvider   string
		embedProvider string
		wantErr       bool
	}{
		{"claude requires an embedding provider", "claude", "", true},
		{"claude with an embedding provider", "claude", "openai", false},
		{"voyage rejected as llm provider", "voyage", "", true},
		{"claude rejected as embedding provider", "gemini", "claude", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				LLM:         config.LLMConfig{Provider: tt.llmProvider},
				VectorStore: config.VectorStore{Provider: tt.embedProvider},
			}
			if err := validateProviderConfig(cfg); (err != nil) != tt.wantErr {
				t.Errorf("validateProviderConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateProviderConfig_NonClaudeProvidersUnaffected(t *testing.T) {
	for _, provider := range []string{"openai", "ollama", "gemini"} {
		cfg := &config.Config{
			LLM:         config.LLMConfig{Provider: provider},
			VectorStore: config.VectorStore{Provider: ""},
		}
		if err := validateProviderConfig(cfg); err != nil {
			t.Errorf("provider %q: expected no error with vector_store.provider unset, got: %v", provider, err)
		}
	}
}

func TestWarnIgnoredExcludedHeaders(t *testing.T) {
	tests := []struct {
		name     string
		analysis config.Analysis
		wantWarn bool
	}{
		{name: "both set", analysis: config.Analysis{IncludedHeaders: []string{"Rules"}, ExcludedHeaders: []string{"Context"}}, wantWarn: true},
		{name: "only included", analysis: config.Analysis{IncludedHeaders: []string{"Rules"}}},
		{name: "only excluded", analysis: config.Analysis{ExcludedHeaders: []string{"Context"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			warnIgnoredExcludedHeaders(output.New(&buf, false), tt.analysis)

			if warned := strings.Contains(buf.String(), "excluded_headers"); warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v: %q", warned, tt.wantWarn, buf.String())
			}
		})
	}
}

func TestCompileADRIDPattern_EmptyIsNil(t *testing.T) {
	cfg := &config.Config{}

	re, err := compileADRIDPattern(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if re != nil {
		t.Errorf("re = %v, want nil", re)
	}
}

func TestCompileADRIDPattern_CaptureGroups(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"no group uses the whole match", `^adr-\d+`, false},
		{"one group is the ID", `^adr-(\d+)-`, false},
		{"a non-capturing group does not count", `^(?:adr|ADR)-(\d+)-`, false},
		{"two groups are ambiguous", `^(adr)-(\d+)-`, true},
		{"named groups count too", `^(?P<kind>adr)-(?P<id>\d+)-`, true},
		{"invalid regex errors", `[unterminated`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Analysis.ADRIDPattern = tt.pattern

			re, err := compileADRIDPattern(cfg)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "analysis.adr_id_pattern") {
					t.Fatalf("err = %v, want an error naming analysis.adr_id_pattern", err)
				}

				return
			}

			if err != nil || re == nil {
				t.Fatalf("compileADRIDPattern() = %v, %v; want a compiled pattern", re, err)
			}
		})
	}
}

func TestValidateFrontmatterMappings_EmptyIsNil(t *testing.T) {
	cfg := &config.Config{}

	mappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mappings != nil {
		t.Errorf("mappings = %v, want nil", mappings)
	}
}

func TestValidateFrontmatterMappings_ValidMappingPassesThrough(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "applies_to"}

	mappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mappings["scope"] != "applies_to" {
		t.Errorf("mappings[scope] = %q, want %q", mappings["scope"], "applies_to")
	}
}

func TestValidateFrontmatterMappings_InvalidMappingsError(t *testing.T) {
	tests := []struct {
		name     string
		mappings map[string]string
	}{
		{"unknown canonical field", map[string]string{"scop": "applies_to"}},
		{"two fields mapped to the same key", map[string]string{"scope": "x", "title": "x"}},
		{"mapped key collides with an unmapped field's default key", map[string]string{"scope": "title"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Analysis.FrontmatterMappings = tt.mappings

			if _, err := validateFrontmatterMappings(cfg); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestValidateFrontmatterMappings_RulesFieldCanBeRemapped(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"rules": "screening"}

	mappings, err := validateFrontmatterMappings(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mappings["rules"] != "screening" {
		t.Errorf("mappings[rules] = %q, want %q", mappings["rules"], "screening")
	}
}

func TestValidateFrontmatterMappings_MappedKeyCollidesWithRulesDefaultErrors(t *testing.T) {
	cfg := &config.Config{}

	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "rules"}

	_, err := validateFrontmatterMappings(cfg)

	var collision *mappingCollisionError
	if !errors.As(err, &collision) || collision.defaultKeyField() != "rules" {
		t.Fatalf("error = %v, want a collision with the rules field's default key", err)
	}
}

func TestValidateFrontmatterMappings_CollisionBetweenTwoMappedKeysNamesNoDefaultField(t *testing.T) {
	cfg := &config.Config{}
	cfg.Analysis.FrontmatterMappings = map[string]string{"scope": "x", "title": "x"}

	_, err := validateFrontmatterMappings(cfg)

	var collision *mappingCollisionError
	if !errors.As(err, &collision) || collision.defaultKeyField() != "" {
		t.Fatalf("error = %v, want a collision between two mapped keys", err)
	}
}
