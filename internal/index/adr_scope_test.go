package index

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestScopePatterns_MatchesAnyPattern(t *testing.T) {
	sp := ScopePatterns{"internal/api/**", "internal/handlers/**"}

	if !sp.Matches("internal/api/foo.go") {
		t.Error("expected match against first pattern")
	}
	if !sp.Matches("internal/handlers/bar.go") {
		t.Error("expected match against second pattern")
	}
	if sp.Matches("internal/other/baz.go") {
		t.Error("expected no match for a path matching neither pattern")
	}
}

func TestScopePatterns_EmptyMatchesEverything(t *testing.T) {
	var sp ScopePatterns
	if !sp.Matches("anything/at/all.rb") {
		t.Error("expected nil/empty ScopePatterns to match any path")
	}
}

func TestScopePatterns_UnmarshalYAML_Scalar(t *testing.T) {
	var fm FrontMatter
	err := yaml.Unmarshal([]byte(`scope: "**/*.go"`), &fm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Scope) != 1 || fm.Scope[0] != "**/*.go" {
		t.Errorf("expected single-pattern scope, got %+v", fm.Scope)
	}
}

func TestScopePatterns_UnmarshalYAML_List(t *testing.T) {
	var fm FrontMatter
	yamlDoc := "scope:\n  - \"internal/api/**\"\n  - \"internal/handlers/**\"\n"
	err := yaml.Unmarshal([]byte(yamlDoc), &fm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Scope) != 2 || fm.Scope[0] != "internal/api/**" || fm.Scope[1] != "internal/handlers/**" {
		t.Errorf("expected two patterns, got %+v", fm.Scope)
	}
}

func TestScopePatterns_UnmarshalYAML_Absent(t *testing.T) {
	var fm FrontMatter
	err := yaml.Unmarshal([]byte(`title: "No scope here"`), &fm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Scope) != 0 {
		t.Errorf("expected empty scope when key is absent, got %+v", fm.Scope)
	}
}

func TestScopePatterns_JSONRoundTrip_SinglePattern(t *testing.T) {
	sp := ScopePatterns{"**/*.go"}
	data, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(data) != `"**/*.go"` {
		t.Errorf("expected bare-string JSON for single pattern, got %s", data)
	}

	var got ScopePatterns
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 1 || got[0] != "**/*.go" {
		t.Errorf("expected round-trip to single pattern, got %+v", got)
	}
}

func TestScopePatterns_JSONRoundTrip_MultiPattern(t *testing.T) {
	sp := ScopePatterns{"internal/api/**", "internal/handlers/**"}
	data, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var got ScopePatterns
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 2 || got[0] != "internal/api/**" || got[1] != "internal/handlers/**" {
		t.Errorf("expected round-trip to two patterns, got %+v", got)
	}
}

func TestScopePatterns_JSONUnmarshal_EmptyString(t *testing.T) {
	var got ScopePatterns
	if err := json.Unmarshal([]byte(`""`), &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty ScopePatterns for an empty JSON string, got %+v", got)
	}
}

func TestScopePatterns_ParseAndSerialize_RoundTrip(t *testing.T) {
	cases := []ScopePatterns{
		nil,
		{"**/*.go"},
		{"internal/api/**", "internal/handlers/**"},
	}
	for _, sp := range cases {
		got := ParseScopePatterns(sp.Serialize())
		if len(got) != len(sp) {
			t.Errorf("Serialize/Parse round-trip mismatch for %+v: got %+v", sp, got)
			continue
		}
		for i := range sp {
			if got[i] != sp[i] {
				t.Errorf("Serialize/Parse round-trip mismatch for %+v: got %+v", sp, got)
			}
		}
	}
}

func TestScopePatterns_Parse_LegacyRawText(t *testing.T) {
	// A pre-existing PgStore row stores a single pattern as raw text, not JSON.
	got := ParseScopePatterns("internal/api/**")
	if len(got) != 1 || got[0] != "internal/api/**" {
		t.Errorf("expected legacy raw-text scope to parse as a single pattern, got %+v", got)
	}
}
