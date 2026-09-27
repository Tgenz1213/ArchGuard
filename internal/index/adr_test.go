package index

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

func TestSplitFrontMatter_MatchesSplitNForWellFormedFiles(t *testing.T) {
	inputs := []string{
		"---\ntitle: T\nstatus: Accepted\n---\nBody",
		"---\ntitle: T\n---\n\n## Context\n\nText with --- inside the body\n---\nmore",
		"---\r\ntitle: T\r\n---\r\nCRLF body\r\n",
		"---\n---\nEmpty frontmatter",
		"---\ntitle: T\n---",
		"---\ntitle: T\n---trailing text on the closing line",
	}
	for _, in := range inputs {
		data := []byte(in)
		want := bytes.SplitN(data, []byte("---"), 3)

		frontMatter, body, ok := splitFrontMatter(data)
		if !ok || !bytes.Equal(frontMatter, want[1]) || !bytes.Equal(body, want[2]) {
			t.Errorf("splitFrontMatter(%q) = (%q, %q, %v), want (%q, %q, true)", in, frontMatter, body, ok, want[1], want[2])
		}
	}
}

func TestSplitFrontMatter_NoClosingFence(t *testing.T) {
	if _, _, ok := splitFrontMatter([]byte("---\ntitle: T\nno closing fence")); ok {
		t.Error("expected ok=false without a closing fence")
	}
}

func TestParseADR_DashesInsideFrontMatterValues(t *testing.T) {
	tests := []struct {
		name        string
		frontMatter string
		wantTitle   string
		wantRules   Rules
	}{
		{"quoted --- in a rule example", "title: T\nrules:\n  - statement: S\n    violating: [\"a --- b\"]\n", "T", Rules{{Statement: "S", Violating: []string{"a --- b"}}}},
		{"unquoted --- in a rule statement", "title: T\nrules:\n  - No separators like --- here\n", "T", Rules{{Statement: "No separators like --- here"}}},
		{"--- in the title", "title: \"Pros --- cons\"\n", "Pros --- cons", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("---\nstatus: Accepted\n" + tt.frontMatter + "---\nBody")

			adr, rulesErr, err := parseADR(data, "1", "x.md", ParseOptions{}, nil)
			if err != nil || rulesErr != nil {
				t.Fatalf("parseADR err = %v, rulesErr = %v", err, rulesErr)
			}

			if adr.Title != tt.wantTitle || !reflect.DeepEqual(adr.Rules, tt.wantRules) || adr.Content != "\nBody" {
				t.Errorf("got title %q, rules %+v, content %q", adr.Title, adr.Rules, adr.Content)
			}
		})
	}
}

func TestParseADRContent_SimilarityThresholdOverride(t *testing.T) {
	data := []byte("---\ntitle: \"Strict ADR\"\nstatus: \"Accepted\"\nsimilarity_threshold: 0.6\n---\nBody")

	adr, err := ParseADRContent(data, "0001", "0001-strict.md", ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adr.SimilarityThreshold == nil || *adr.SimilarityThreshold != 0.6 {
		t.Fatalf("expected similarity_threshold override of 0.6, got %+v", adr.SimilarityThreshold)
	}
}

func TestParseADRContent_SimilarityThresholdUnsetIsNil(t *testing.T) {
	data := []byte("---\ntitle: \"Default ADR\"\nstatus: \"Accepted\"\n---\nBody")

	adr, err := ParseADRContent(data, "0002", "0002-default.md", ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adr.SimilarityThreshold != nil {
		t.Fatalf("expected nil similarity_threshold when frontmatter omits it, got %v", *adr.SimilarityThreshold)
	}
}

func TestParseADRContent_MultiPatternScope(t *testing.T) {
	data := []byte("---\ntitle: \"Multi Scope\"\nstatus: \"Accepted\"\nscope:\n  - \"internal/api/**\"\n  - \"internal/handlers/**\"\n---\nBody")

	adr, err := ParseADRContent(data, "0001", "0001-multi-scope.md", ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adr.Scope) != 2 || adr.Scope[0] != "internal/api/**" || adr.Scope[1] != "internal/handlers/**" {
		t.Fatalf("expected two scope patterns, got %+v", adr.Scope)
	}
}

func TestParseADRContent_CustomScopeKeyMappedIsReadAsScope(t *testing.T) {
	data := []byte("---\ntitle: \"Custom Scope Key\"\nstatus: \"Accepted\"\napplies_to: \"**/*.go\"\n---\nBody")
	mappings := map[string]string{"scope": "applies_to"}

	adr, err := ParseADRContent(data, "0001", "0001-custom-scope.md", ParseOptions{FrontmatterMappings: mappings})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adr.Scope) != 1 || adr.Scope[0] != "**/*.go" {
		t.Fatalf("expected scope [\"**/*.go\"] via mapped key, got %+v", adr.Scope)
	}
}

func TestParseADRContent_CustomScopeKeyUnmappedFallsBackToUnrestrictedScope(t *testing.T) {
	data := []byte("---\ntitle: \"Custom Scope Key\"\nstatus: \"Accepted\"\napplies_to: \"**/*.go\"\n---\nBody")

	adr, err := ParseADRContent(data, "0001", "0001-custom-scope.md", ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(adr.Scope) != 0 {
		t.Fatalf("expected unrestricted (empty) scope when mapping absent, got %+v", adr.Scope)
	}
}

func TestParseADRContent_CustomSimilarityThresholdKeyMapped(t *testing.T) {
	data := []byte("---\ntitle: \"Custom Threshold Key\"\nstatus: \"Accepted\"\nconfidence: 0.42\n---\nBody")
	mappings := map[string]string{"similarity_threshold": "confidence"}

	adr, err := ParseADRContent(data, "0001", "0001-custom-threshold.md", ParseOptions{FrontmatterMappings: mappings})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adr.SimilarityThreshold == nil || *adr.SimilarityThreshold != 0.42 {
		t.Fatalf("expected similarity_threshold of 0.42 via mapped key, got %+v", adr.SimilarityThreshold)
	}
}

func TestParseADRContent_MappedSimilarityThresholdExplicitNullStaysNil(t *testing.T) {
	data := []byte("---\ntitle: \"Explicit Null Threshold\"\nstatus: \"Accepted\"\nconfidence: null\n---\nBody")
	mappings := map[string]string{"similarity_threshold": "confidence"}

	adr, err := ParseADRContent(data, "0001", "0001-null-threshold.md", ParseOptions{FrontmatterMappings: mappings})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adr.SimilarityThreshold != nil {
		t.Fatalf("expected nil similarity_threshold for an explicit null at the mapped key, got %v", *adr.SimilarityThreshold)
	}
}

func TestParseADRContent_UnmappedFieldKeepsDefaultKeyWithOneFieldRemapped(t *testing.T) {
	data := []byte("---\ntitle: \"Title Stays Default\"\nstatus: \"Accepted\"\napplies_to: \"internal/**\"\n---\nBody")
	mappings := map[string]string{"scope": "applies_to"}

	adr, err := ParseADRContent(data, "0001", "0001-mixed.md", ParseOptions{FrontmatterMappings: mappings})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adr.Title != "Title Stays Default" {
		t.Errorf("Title = %q, want %q (unmapped field should keep reading its default key)", adr.Title, "Title Stays Default")
	}

	if adr.Status != "Accepted" {
		t.Errorf("Status = %q, want %q", adr.Status, "Accepted")
	}

	if len(adr.Scope) != 1 || adr.Scope[0] != "internal/**" {
		t.Fatalf("expected scope [\"internal/**\"] via mapped key, got %+v", adr.Scope)
	}
}

func TestParseADR_DefaultSplitBehaviorUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0001-use-postgres.md")

	content := "---\ntitle: Use Postgres\nstatus: Accepted\n---\nBody"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	adr, err := ParseADR(path, dir, nil, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	if adr.ID != "0001" {
		t.Errorf("ID = %q, want %q", adr.ID, "0001")
	}
}

func TestParseADR_CustomPatternDistinguishesCollidingDefaultIDs(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"adr-1-use-postgres.md": "---\ntitle: Use Postgres\nstatus: Accepted\n---\nBody",
		"adr-2-use-kafka.md":    "---\ntitle: Use Kafka\nstatus: Accepted\n---\nBody",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pattern := regexp.MustCompile(`^adr-(\d+)-`)

	adr1, err := ParseADR(filepath.Join(dir, "adr-1-use-postgres.md"), dir, pattern, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	adr2, err := ParseADR(filepath.Join(dir, "adr-2-use-kafka.md"), dir, pattern, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	if adr1.ID != "1" {
		t.Errorf("adr1.ID = %q, want %q", adr1.ID, "1")
	}

	if adr2.ID != "2" {
		t.Errorf("adr2.ID = %q, want %q", adr2.ID, "2")
	}

	if adr1.ID == adr2.ID {
		t.Errorf("adr1.ID and adr2.ID both = %q, want distinct IDs", adr1.ID)
	}
}

func TestParseADR_CustomPatternNoMatchFallsBackToDefaultSplit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0003-unrelated.md")

	content := "---\ntitle: Unrelated\nstatus: Accepted\n---\nBody"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pattern := regexp.MustCompile(`^adr-(\d+)-`)

	adr, err := ParseADR(path, dir, pattern, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	if adr.ID != "0003" {
		t.Errorf("ID = %q, want %q (fallback to default split)", adr.ID, "0003")
	}
}

func TestParseADR_PatternWithEmptyCaptureGroupFallsBackToDefaultSplit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0005-empty-capture.md")

	content := "---\ntitle: Empty Capture\nstatus: Accepted\n---\nBody"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pattern := regexp.MustCompile(`^0005(x?)-`)

	adr, err := ParseADR(path, dir, pattern, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	if adr.ID != "0005" {
		t.Errorf("ID = %q, want %q (fallback to default split when capture group is empty)", adr.ID, "0005")
	}
}

func TestParseADR_PatternWithoutCaptureGroupUsesWholeMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adr-7-use-redis.md")

	content := "---\ntitle: Use Redis\nstatus: Accepted\n---\nBody"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pattern := regexp.MustCompile(`^adr-\d+`)

	adr, err := ParseADR(path, dir, pattern, ParseOptions{})
	if err != nil {
		t.Fatalf("ParseADR failed: %v", err)
	}

	if adr.ID != "adr-7" {
		t.Errorf("ID = %q, want %q", adr.ID, "adr-7")
	}
}
