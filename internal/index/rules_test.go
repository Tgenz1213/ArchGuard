package index

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func parseWithFrontMatter(t *testing.T, frontMatter string, opts ParseOptions) *ADR {
	t.Helper()
	data := []byte("---\ntitle: \"T\"\nstatus: \"Accepted\"\n" + frontMatter + "---\nBody")

	adr, err := ParseADRContent(data, "0001", "0001-t.md", opts)
	if err != nil {
		t.Fatalf("ParseADRContent failed: %v", err)
	}

	return adr
}

func TestParseADRContent_FrontMatterRules(t *testing.T) {
	tests := []struct {
		name        string
		frontMatter string
		want        Rules
		wantErr     string
	}{
		{
			name: "full rule",
			frontMatter: "rules:\n" +
				"  - statement: No direct DB access in handlers\n" +
				"    violating:\n      - imports app/db\n      - calls sql.Open\n" +
				"    compliant:\n      - uses the repository\n",
			want: Rules{{
				Statement: "No direct DB access in handlers",
				Violating: []string{"imports app/db", "calls sql.Open"},
				Compliant: []string{"uses the repository"},
			}},
		},
		{
			name:        "statement only",
			frontMatter: "rules:\n  - statement: Only a statement\n",
			want:        Rules{{Statement: "Only a statement"}},
		},
		{
			name:        "lone string example becomes one-item list",
			frontMatter: "rules:\n  - statement: S\n    violating: bad code\n    compliant: good code\n",
			want:        Rules{{Statement: "S", Violating: []string{"bad code"}, Compliant: []string{"good code"}}},
		},
		{
			name:        "empty example list means no examples",
			frontMatter: "rules:\n  - statement: S\n    violating: []\n",
			want:        Rules{{Statement: "S"}},
		},
		{
			name:        "block scalar example keeps its inner newline",
			frontMatter: "rules:\n  - statement: S\n    violating: |\n      line one\n      line two\n",
			want:        Rules{{Statement: "S", Violating: []string{"line one\nline two"}}},
		},
		{
			name:        "aliased rule is expanded",
			frontMatter: "rules:\n  - &shared\n    statement: shared\n  - *shared\n",
			want:        Rules{{Statement: "shared"}, {Statement: "shared"}},
		},
		{
			name:        "several rules keep order",
			frontMatter: "rules:\n  - statement: first\n  - statement: second\n",
			want:        Rules{{Statement: "first"}, {Statement: "second"}},
		},
		{name: "absent key", frontMatter: "", want: nil},
		{name: "explicit null", frontMatter: "rules:\n", want: nil},
		{name: "empty list", frontMatter: "rules: []\n", want: nil},
		{name: "not a list", frontMatter: "rules: nope\n", wantErr: "rules must be a list"},
		{name: "item not a mapping", frontMatter: "rules:\n  - just a string\n", wantErr: "rule 1: must be a mapping"},
		{name: "missing statement", frontMatter: "rules:\n  - violating: x\n", wantErr: "rule 1: statement is required"},
		{name: "blank statement", frontMatter: "rules:\n  - statement: \"  \"\n", wantErr: "rule 1: statement is required"},
		{name: "unknown key", frontMatter: "rules:\n  - statement: S\n    severity: high\n", wantErr: `rule 1: unknown key "severity"`},
		{name: "example wrong type", frontMatter: "rules:\n  - statement: S\n    violating:\n      a: b\n", wantErr: "rule 1: violating must be a string or a list of strings"},
		{name: "blank example", frontMatter: "rules:\n  - statement: S\n    compliant:\n      - \"\"\n", wantErr: "rule 1: compliant contains an empty example"},
		{name: "error names the failing rule", frontMatter: "rules:\n  - statement: ok\n  - statement: \"\"\n", wantErr: "rule 2: statement is required"},
	}
	decodePaths := map[string]ParseOptions{
		"default decode": {},
		"mapped decode":  {FrontmatterMappings: map[string]string{"scope": "applies_to"}},
	}
	for _, tt := range tests {
		for pathName, opts := range decodePaths {
			t.Run(tt.name+"/"+pathName, func(t *testing.T) {
				adr := parseWithFrontMatter(t, tt.frontMatter, opts)
				if tt.wantErr != "" {
					if adr.Rules != nil {
						t.Errorf("expected no rules for malformed input, got %+v", adr.Rules)
					}

					if !strings.Contains(adr.RulesError, tt.wantErr) {
						t.Errorf("RulesError = %q, want it to contain %q", adr.RulesError, tt.wantErr)
					}

					return
				}

				if adr.RulesError != "" {
					t.Errorf("unexpected RulesError %q", adr.RulesError)
				}

				if !reflect.DeepEqual(adr.Rules, tt.want) {
					t.Errorf("Rules = %+v, want %+v", adr.Rules, tt.want)
				}
			})
		}
	}
}

func TestParseADRContent_MalformedRulesKeepTheRestOfTheADR(t *testing.T) {
	adr := parseWithFrontMatter(t, "scope: \"**/*.go\"\nrules: nope\n", ParseOptions{})
	if adr.Title != "T" || adr.Status != "Accepted" || len(adr.Scope) != 1 {
		t.Fatalf("ADR fields lost alongside malformed rules: %+v", adr)
	}
}

func TestParseADRContent_RulesKeyRemapped(t *testing.T) {
	opts := ParseOptions{FrontmatterMappings: map[string]string{"rules": "screening"}}

	adr := parseWithFrontMatter(t, "screening:\n  - statement: mapped\nrules:\n  - statement: ignored\n", opts)
	if want := (Rules{{Statement: "mapped"}}); !reflect.DeepEqual(adr.Rules, want) {
		t.Errorf("Rules = %+v, want %+v", adr.Rules, want)
	}
}

func TestParseADRContent_RulesUnmappedKeyStillReadWhenOtherFieldRemapped(t *testing.T) {
	opts := ParseOptions{FrontmatterMappings: map[string]string{"scope": "applies_to"}}

	adr := parseWithFrontMatter(t, "applies_to: \"**/*.go\"\nrules:\n  - statement: canonical\n", opts)
	if want := (Rules{{Statement: "canonical"}}); !reflect.DeepEqual(adr.Rules, want) {
		t.Errorf("Rules = %+v, want %+v", adr.Rules, want)
	}
}

func TestRules_ValueScanRoundTrip(t *testing.T) {
	in := Rules{{Statement: "S", Violating: []string{"v"}, Compliant: []string{"c1", "c2"}}, {Statement: "T"}}

	v, err := in.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var out Rules
	if err := out.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestRules_EmptyIsNullInTheDatabase(t *testing.T) {
	for _, r := range []Rules{nil, {}} {
		v, err := r.Value()
		if err != nil || v != nil {
			t.Errorf("Value() of %#v = (%v, %v), want (nil, nil)", r, v, err)
		}
	}

	for _, src := range []any{nil, "", []byte(nil)} {
		out := Rules{{Statement: "stale"}}
		if err := out.Scan(src); err != nil || out != nil {
			t.Errorf("Scan(%#v) = (%+v, %v), want (nil, nil)", src, out, err)
		}
	}
}

func TestRules_ScanRejectsUnsupportedType(t *testing.T) {
	var out Rules
	if err := out.Scan(42); err == nil {
		t.Error("expected an error scanning an int")
	}
}

func TestADR_JSONOmitsRulesWhenEmpty(t *testing.T) {
	for _, adr := range []ADR{{ID: "1", Title: "T"}, {ID: "1", Title: "T", Rules: Rules{}}} {
		data, err := json.Marshal(adr)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(data), "rules") {
			t.Errorf("ADR without rules must not emit a rules key, got %s", data)
		}
	}
}

func TestADR_JSONRoundTripsRulesAndDropsRulesError(t *testing.T) {
	in := ADR{ID: "1", Rules: Rules{{Statement: "S"}}, RulesError: "should not persist"}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(data), "should not persist") {
		t.Errorf("RulesError leaked into JSON: %s", data)
	}

	var out ADR
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(out.Rules, in.Rules) || out.RulesError != "" {
		t.Errorf("round trip = %+v", out)
	}
}
