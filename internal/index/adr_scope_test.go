package index

import (
	"encoding/json"
	"slices"
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

func TestScopePatterns_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{name: "scalar", yaml: `scope: "**/*.go"`, want: []string{"**/*.go"}},
		{
			name: "list",
			yaml: "scope:\n  - \"internal/api/**\"\n  - \"internal/handlers/**\"\n",
			want: []string{"internal/api/**", "internal/handlers/**"},
		},
		{name: "absent", yaml: `title: "No scope here"`, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fm FrontMatter
			if err := yaml.Unmarshal([]byte(tt.yaml), &fm); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !slices.Equal(fm.Scope, tt.want) {
				t.Errorf("scope = %+v, want %+v", fm.Scope, tt.want)
			}
		})
	}
}

func TestScopePatterns_JSONRoundTrip_SinglePattern(t *testing.T) {
	sp := ScopePatterns{"**/*.go"}

	encoded, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if string(encoded) != `"**/*.go"` {
		t.Errorf("expected bare-string JSON for single pattern, got %s", encoded)
	}

	var got ScopePatterns
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(got) != 1 || got[0] != "**/*.go" {
		t.Errorf("expected round-trip to single pattern, got %+v", got)
	}
}

func TestScopePatterns_JSONRoundTrip_MultiPattern(t *testing.T) {
	sp := ScopePatterns{"internal/api/**", "internal/handlers/**"}

	encoded, err := json.Marshal(sp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var got ScopePatterns
	if err := json.Unmarshal(encoded, &got); err != nil {
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

func TestScopePatterns_Parse_LiteralJSONScalarIsNotMisreadAsArray(t *testing.T) {
	// A glob literally named "null" (or any bare JSON scalar) must survive
	// as a single pattern, not be JSON-sniffed into an empty/nil result.
	cases := []string{"null", "true", "false", "0"}
	for _, s := range cases {
		got := ParseScopePatterns(s)
		if len(got) != 1 || got[0] != s {
			t.Errorf("ParseScopePatterns(%q): expected single literal pattern, got %+v", s, got)
		}
	}
}
