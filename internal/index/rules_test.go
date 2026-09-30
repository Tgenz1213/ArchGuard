package index

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func parseWithFrontMatter(t *testing.T, frontMatter string, opts ParseOptions) (*ADR, error) {
	t.Helper()
	data := []byte("---\ntitle: \"T\"\nstatus: \"Accepted\"\n" + frontMatter + "---\nBody")

	adr, rulesErr, err := parseADR(data, "0001", "0001-t.md", opts, nil)
	if err != nil {
		t.Fatalf("parseADR failed: %v", err)
	}

	return adr, rulesErr
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
		{name: "plain string items are statements", frontMatter: "rules:\n  - All code MUST be written in Go.\n  - \"Quoted, with a comma\"\n", want: statements("All code MUST be written in Go.", "Quoted, with a comma")},
		{name: "strings and mappings mix", frontMatter: "rules:\n  - Bare statement\n  - statement: Mapped\n    violating: bad()\n", want: Rules{{Statement: "Bare statement"}, {Statement: "Mapped", Violating: []string{"bad()"}}}},
		{name: "blank string item", frontMatter: "rules:\n  - ok\n  - \"  \"\n", wantErr: "rule 2: statement is required"},
		{name: "boolean item", frontMatter: "rules:\n  - ok\n  - true\n", wantErr: "rule 2: statement must be a string"},
		{name: "numeric item", frontMatter: "rules:\n  - 123\n", wantErr: "rule 1: statement must be a string"},
		{name: "date item", frontMatter: "rules:\n  - 2024-01-01\n", wantErr: "rule 1: statement must be a string"},
		{name: "numeric statement key", frontMatter: "rules:\n  - statement: 42\n", wantErr: "rule 1: statement must be a string"},
		{name: "quoted number is a string", frontMatter: "rules:\n  - '123'\n", want: statements("123")},
		{name: "numeric examples keep their source text", frontMatter: "rules:\n  - statement: No magic numbers\n    violating: [86400, 0x1F, 3.14]\n", want: Rules{{Statement: "No magic numbers", Violating: []string{"86400", "0x1F", "3.14"}}}},
		{name: "null item", frontMatter: "rules:\n  - ~\n", wantErr: "rule 1: must be a string or a mapping"},
		{name: "item is a nested list", frontMatter: "rules:\n  - [a, b]\n", wantErr: "rule 1: must be a string or a mapping"},
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
				adr, rulesErr := parseWithFrontMatter(t, tt.frontMatter, opts)
				if tt.wantErr != "" {
					if adr.Rules != nil {
						t.Errorf("expected no rules for malformed input, got %+v", adr.Rules)
					}

					if rulesErr == nil || !strings.Contains(rulesErr.Error(), tt.wantErr) {
						t.Errorf("rulesErr = %v, want it to contain %q", rulesErr, tt.wantErr)
					}

					return
				}

				if rulesErr != nil {
					t.Errorf("unexpected rulesErr %v", rulesErr)
				}

				if !reflect.DeepEqual(adr.Rules, tt.want) {
					t.Errorf("Rules = %+v, want %+v", adr.Rules, tt.want)
				}
			})
		}
	}
}

func TestParseADRContent_MalformedRulesKeepTheRestOfTheADR(t *testing.T) {
	adr, rulesErr := parseWithFrontMatter(t, "scope: \"**/*.go\"\nrules: nope\n", ParseOptions{})
	if rulesErr == nil {
		t.Fatal("expected a rules error for malformed rules")
	}

	if adr.Title != "T" || adr.Status != "Accepted" || len(adr.Scope) != 1 {
		t.Fatalf("ADR fields lost alongside malformed rules: %+v", adr)
	}
}

func TestParseADRContent_ExportedWrapperStillReturnsTheADRWhenRulesAreMalformed(t *testing.T) {
	data := []byte("---\ntitle: \"T\"\nstatus: \"Accepted\"\nrules: nope\n---\nBody")

	adr, err := ParseADRContent(data, "0001", "0001-t.md", ParseOptions{})
	if err != nil || adr == nil || adr.Title != "T" || adr.Rules != nil {
		t.Fatalf("ParseADRContent = (%+v, %v), want the ADR without rules and no error", adr, err)
	}
}

func TestParseADRContent_RulesKeyRemapped(t *testing.T) {
	opts := ParseOptions{FrontmatterMappings: map[string]string{"rules": "screening"}}

	adr, rulesErr := parseWithFrontMatter(t, "screening:\n  - statement: mapped\nrules:\n  - statement: ignored\n", opts)
	if rulesErr != nil {
		t.Fatalf("rules error: %v", rulesErr)
	}

	if want := (Rules{{Statement: "mapped"}}); !reflect.DeepEqual(adr.Rules, want) {
		t.Errorf("Rules = %+v, want %+v", adr.Rules, want)
	}
}

func TestParseADRContent_RulesUnmappedKeyStillReadWhenOtherFieldRemapped(t *testing.T) {
	opts := ParseOptions{FrontmatterMappings: map[string]string{"scope": "applies_to"}}

	adr, rulesErr := parseWithFrontMatter(t, "applies_to: \"**/*.go\"\nrules:\n  - statement: canonical\n", opts)
	if rulesErr != nil {
		t.Fatalf("rules error: %v", rulesErr)
	}

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

func TestADR_JSONRoundTripsRules(t *testing.T) {
	in := ADR{ID: "1", Rules: Rules{{Statement: "S", Violating: []string{"v"}}}}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	var out ADR
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(out.Rules, in.Rules) {
		t.Errorf("round trip = %+v, want %+v", out.Rules, in.Rules)
	}
}

func statements(texts ...string) Rules {
	rules := make(Rules, len(texts))
	for i, s := range texts {
		rules[i] = Rule{Statement: s}
	}

	return rules
}

func TestExtractBodyRules(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		body    string
		want    Rules
		wantErr string
	}{
		{
			name:    "typical rules section",
			heading: "Rules",
			body: "## Rules\n\n" +
				"- All code MUST be written in Go.\n" +
				"- There MUST NOT be hand rolled logic where a well-tested library exists to solve the same problem\n",
			want: statements(
				"All code MUST be written in Go.",
				"There MUST NOT be hand rolled logic where a well-tested library exists to solve the same problem",
			),
		},
		{
			name:    "commas stay inside the statement",
			heading: "Rules",
			body:    "## Rules\n- Handlers must not import the db package, or call sql.Open, directly\n",
			want:    statements("Handlers must not import the db package, or call sql.Open, directly"),
		},
		{
			name:    "star, plus and numbered lists",
			heading: "Rules",
			body:    "## Rules\n* A\n\n+ B\n\n1. C\n2. D\n",
			want:    statements("A", "B", "C", "D"),
		},
		{
			name:    "loose list",
			heading: "Rules",
			body:    "## Rules\n\n- A\n\n- B\n",
			want:    statements("A", "B"),
		},
		{
			name:    "wrapped statement is joined",
			heading: "Rules",
			body:    "## Rules\n- A long statement\n  that wraps\n- B\n",
			want:    statements("A long statement that wraps", "B"),
		},
		{
			name:    "inline markup is kept",
			heading: "Rules",
			body:    "## Rules\n- Use `errors.Is` and **never** `==`\n",
			want:    statements("Use `errors.Is` and **never** `==`"),
		},
		{
			name:    "nested bullets are ignored",
			heading: "Rules",
			body:    "## Rules\n- Parent\n  - detail one\n  - detail two\n- Next\n",
			want:    statements("Parent", "Next"),
		},
		{
			name:    "section ends at a same-level heading",
			heading: "Rules",
			body:    "## Rules\n- In\n## Consequences\n- Out\n",
			want:    statements("In"),
		},
		{
			name:    "section ends at a higher-level heading",
			heading: "Rules",
			body:    "## Rules\n- In\n# Top\n- Out\n",
			want:    statements("In"),
		},
		{
			name:    "deeper subheading does not end the section",
			heading: "Rules",
			body:    "## Rules\n- One\n### Sub\n- Two\n",
			want:    statements("One", "Two"),
		},
		{
			name:    "heading level and case do not matter",
			heading: "Rules",
			body:    "### rULES\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "closing hashes are ignored",
			heading: "Rules",
			body:    "## Rules ##\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "setext heading",
			heading: "Rules",
			body:    "Rules\n-----\n\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "custom heading",
			heading: "Screening Rules",
			body:    "## Screening Rules\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "heading with a trailing colon",
			heading: "Rules",
			body:    "## Rules:\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "bold heading",
			heading: "Rules",
			body:    "## **Rules**\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "code-span heading",
			heading: "Rules",
			body:    "## `Rules`\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "partly emphasized heading",
			heading: "Screening Rules",
			body:    "## Screening *Rules*\n- R\n",
			want:    statements("R"),
		},
		{
			name:    "bare checkbox is an empty bullet",
			heading: "Rules",
			body:    "## Rules\n- R\n- [ ]\n",
			wantErr: "rule 2: bullet has no statement text",
		},
		{
			name:    "a colon inside the heading is not stripped",
			heading: "Rules",
			body:    "## Rules: overview\n- R\n",
		},
		{
			name:    "task-list markers are dropped",
			heading: "Rules",
			body:    "## Rules\n- [ ] Open\n- [x] Done\n- [X] Also done\n- [link] stays\n",
			want:    statements("Open", "Done", "Also done", "[link] stays"),
		},
		{
			name:    "a longer heading is not a match",
			heading: "Rules",
			body:    "## Rules Overview\n- R\n",
		},
		{
			name:    "no section",
			heading: "Rules",
			body:    "## Context\n- something\n",
		},
		{
			name:    "heading inside a code fence is not a heading",
			heading: "Rules",
			body:    "```\n## Rules\n- R\n```\n",
		},
		{
			name:    "bullets inside a code fence are not rules",
			heading: "Rules",
			body:    "## Rules\n- Real\n\n```\n- Not a rule\n```\n",
			want:    statements("Real"),
		},
		{
			name:    "prose around the bullets is ignored",
			heading: "Rules",
			body:    "## Rules\n\nThese are the rules.\n\n- R\n\nClosing remark.\n",
			want:    statements("R"),
		},
		{
			name:    "section without bullets means no rules",
			heading: "Rules",
			body:    "## Rules\n\nJust prose.\n",
		},
		{
			name:    "CRLF line endings",
			heading: "Rules",
			body:    "## Rules\r\n\r\n- A\r\n- B\r\n",
			want:    statements("A", "B"),
		},
		{
			name:    "only the first matching section is used",
			heading: "Rules",
			body:    "## Rules\n- First\n## Rules\n- Second\n",
			want:    statements("First"),
		},
		{
			name:    "bullet directly after the heading",
			heading: "Rules",
			body:    "## Rules\n- A\n- B\n",
			want:    statements("A", "B"),
		},
		{
			name:    "list before the heading is not in the section",
			heading: "Rules",
			body:    "- Before\n\n## Rules\n- After\n",
			want:    statements("After"),
		},
		{
			name:    "list inside a block quote is ignored",
			heading: "Rules",
			body:    "## Rules\n\n> - Quoted\n\n- Real\n",
			want:    statements("Real"),
		},
		{
			name:    "bold pseudo-heading is not a heading",
			heading: "Rules",
			body:    "**Rules:**\n\n- R\n",
		},
		{
			name:    "empty bullet",
			heading: "Rules",
			body:    "## Rules\n- A\n-\n- C\n",
			wantErr: "rule 2: bullet has no statement text",
		},
		{
			name:    "bullet that starts with a code block",
			heading: "Rules",
			body:    "## Rules\n- ```\n  code\n  ```\n- ok\n",
			wantErr: "rule 1: bullet has no statement text",
		},
		{
			name:    "bullet that only holds a nested list",
			heading: "Rules",
			body:    "## Rules\n- - inner\n- ok\n",
			wantErr: "rule 1: bullet has no statement text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractBodyRules(tt.body, tt.heading)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}

				if got != nil {
					t.Errorf("rules = %+v, want nil alongside an error", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rules = %+v, want %+v", got, tt.want)
			}
		})
	}
}

const bodySection = "\n## Rules\n\n- Body rule\n"

func parseWithBody(t *testing.T, frontMatter, body string, opts ParseOptions) (*ADR, error) {
	t.Helper()
	data := []byte("---\ntitle: \"T\"\nstatus: \"Accepted\"\n" + frontMatter + "---\nIntro\n" + body)

	adr, rulesErr, err := parseADR(data, "0001", "0001-t.md", opts, nil)
	if err != nil {
		t.Fatalf("parseADR failed: %v", err)
	}

	return adr, rulesErr
}

func TestParseADRContent_RulesResolution(t *testing.T) {
	wantBodyRules := statements("Body rule")
	fmRules := statements("Frontmatter rule")
	const fmYAML = "rules:\n  - statement: Frontmatter rule\n"

	tests := []struct {
		name        string
		frontMatter string
		body        string
		opts        ParseOptions
		want        Rules
		wantErr     string
	}{
		{name: "body only", body: bodySection, want: wantBodyRules},
		{name: "frontmatter only", frontMatter: fmYAML, want: fmRules},
		{name: "both places: frontmatter wins", frontMatter: fmYAML, body: bodySection, want: fmRules},
		{name: "empty frontmatter list falls through to body", frontMatter: "rules: []\n", body: bodySection, want: wantBodyRules},
		{name: "null frontmatter falls through to body", frontMatter: "rules:\n", body: bodySection, want: wantBodyRules},
		{name: "malformed frontmatter does not fall back to body", frontMatter: "rules: nope\n", body: bodySection, wantErr: "frontmatter: rules must be a list"},
		{name: "malformed body", body: "\n## Rules\n\n-\n", wantErr: `"Rules" section: rule 1: bullet has no statement text`},
		{name: "neither place", body: "\n## Context\n\ntext\n", want: nil},
		{name: "custom heading", body: "\n## Detection\n- Custom\n", opts: ParseOptions{RulesHeading: "Detection"}, want: statements("Custom")},
		{name: "custom heading ignores the default one", body: bodySection, opts: ParseOptions{RulesHeading: "Detection"}, want: nil},
		{name: "blank heading option means the default", body: bodySection, opts: ParseOptions{RulesHeading: "  "}, want: wantBodyRules},
		{name: "heading option with a trailing colon matches", body: "\n## Rules:\n- R\n", opts: ParseOptions{RulesHeading: "Rules:"}, want: statements("R")},
		{name: "heading option is trimmed", body: "\n## Detection\n- Custom\n", opts: ParseOptions{RulesHeading: " Detection "}, want: statements("Custom")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adr, rulesErr := parseWithBody(t, tt.frontMatter, tt.body, tt.opts)
			if tt.wantErr != "" {
				if adr.Rules != nil || rulesErr == nil || !strings.Contains(rulesErr.Error(), tt.wantErr) {
					t.Fatalf("Rules = %+v, rulesErr = %v, want no rules and an error containing %q", adr.Rules, rulesErr, tt.wantErr)
				}

				return
			}

			if rulesErr != nil {
				t.Fatalf("unexpected rulesErr %v", rulesErr)
			}

			if !reflect.DeepEqual(adr.Rules, tt.want) {
				t.Errorf("Rules = %+v, want %+v", adr.Rules, tt.want)
			}
		})
	}
}

func TestParseADRContent_RulesSectionStaysInContent(t *testing.T) {
	adr, rulesErr := parseWithBody(t, "", bodySection, ParseOptions{})
	if rulesErr != nil {
		t.Fatalf("rules error: %v", rulesErr)
	}

	if !strings.Contains(adr.Content, "## Rules") {
		t.Errorf("the rules section must remain in Content so LLM prompts are unchanged, got %q", adr.Content)
	}
}

func TestParseADRContent_ADRWithoutRulesSectionLoadsAsBefore(t *testing.T) {
	adr, rulesErr := parseWithBody(t, "scope: \"**/*.go\"\n", "\n## Notes\n\n- a bullet list under another heading\n", ParseOptions{})
	if adr.Rules != nil || rulesErr != nil {
		t.Errorf("Rules = %+v, rulesErr = %v, want neither", adr.Rules, rulesErr)
	}
}
