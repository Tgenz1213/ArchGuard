package agents

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const agentsDir = "../../.claude/agents"

// readOnlyMCPTools are the MCP tools a read-only agent may list. Add a tool
// here only if it cannot write files. Context7 is listed under the name a
// cloud session gives it and the name a local claude.ai connector gives it.
var readOnlyMCPTools = []string{
	"mcp__Context7__resolve-library-id",
	"mcp__Context7__query-docs",
	"mcp__claude_ai_Context7__resolve-library-id",
	"mcp__claude_ai_Context7__query-docs",
	"mcp__github__issue_read",
	"mcp__github__list_issues",
	"mcp__github__search_issues",
	"mcp__github__pull_request_read",
	"mcp__github__list_pull_requests",
	"mcp__github__search_pull_requests",
}

type hookCommand struct {
	Command string `yaml:"command"`
}

type hookMatcher struct {
	Matcher string        `yaml:"matcher"`
	Hooks   []hookCommand `yaml:"hooks"`
}

type agent struct {
	Tools     yaml.Node `yaml:"tools"`
	Isolation string    `yaml:"isolation"`
	Hooks     struct {
		PreToolUse []hookMatcher `yaml:"PreToolUse"`
	} `yaml:"hooks"`
}

func TestAgentsFollowTheirADRs(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(agentsDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}

	if len(files) == 0 {
		t.Fatal("no agents found")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			for _, violation := range violations(t, parseAgent(t, data)) {
				t.Error(violation)
			}
		})
	}
}

func TestProberViolationsCatchEachRule(t *testing.T) {
	const proberHook = `hooks:
  PreToolUse:
    - matcher: Bash
      hooks:
        - type: command
          command: bash "$CLAUDE_PROJECT_DIR/.claude/hooks/prober-bash.sh"
`

	const tools = "tools: Read, Grep, Glob, Bash, Edit, Write\nisolation: worktree\n"

	tests := []struct {
		name        string
		frontmatter string
		want        int
	}{
		{"prober", tools + proberHook, 0},
		{"without the hook", tools, 1},
		{"with the read-only hook", tools + strings.Replace(proberHook, "prober-bash.sh", "read-only-git.sh", 1), 1},
		{"without a worktree", "tools: Read, Bash, Edit, Write\n" + proberHook, 1},
		{"without Bash", "tools: Read, Edit, Write\nisolation: worktree\n" + proberHook, 1},
		{"with an MCP server pattern", "tools: Read, Bash, Edit, Write, mcp__Context7\nisolation: worktree\n" + proberHook, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("---\n" + tc.frontmatter + "---\nBody.\n")

			if got := proberViolations(t, parseAgent(t, data)); len(got) != tc.want {
				t.Errorf("violations = %q, want %d", got, tc.want)
			}
		})
	}
}

func TestProberFollowsItsADR(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(agentsDir, "prober.md"))
	if err != nil {
		t.Fatal(err)
	}

	for _, violation := range proberViolations(t, parseAgent(t, data)) {
		t.Error(violation)
	}
}

func TestViolationsCatchEachRule(t *testing.T) {
	const readOnlyHook = `hooks:
  PreToolUse:
    - matcher: Bash
      hooks:
        - type: command
          command: bash "$CLAUDE_PROJECT_DIR/.claude/hooks/read-only-git.sh"
`

	tests := []struct {
		name        string
		frontmatter string
		want        int
	}{
		{"reviewer", "tools: Read, Grep, Glob, Bash\n" + readOnlyHook, 0},
		{"reviewer as a YAML list", "tools: [Read, Bash, mcp__Context7__query-docs]\n" + readOnlyHook, 0},
		{"reviewer with Context7", "tools: Read, Bash, mcp__Context7__query-docs, mcp__claude_ai_Context7__query-docs\n" + readOnlyHook, 0},
		{"reviewer with a write-capable MCP tool", "tools: Read, Bash, mcp__serena__create_text_file\n" + readOnlyHook, 1},
		{"reviewer with a server pattern", "tools: Read, Bash, mcp__Context7\n" + readOnlyHook, 1},
		{"reviewer with a server wildcard", "tools: Read, Bash, mcp__Context7__*\n" + readOnlyHook, 1},
		{"reviewer without the hook", "tools: Read, Grep, Glob, Bash\n", 1},
		{"hook on another matcher", "tools: Read, Bash\n" + strings.Replace(readOnlyHook, "matcher: Bash", "matcher: Read", 1), 1},
		{"read-only without Bash", "tools: Read, Grep\n", 0},
		{"writer", "isolation: worktree\n", 0},
		{"no tools without a worktree", "name: x\n", 1},
		{"Edit without a worktree", "tools: Read, Edit\n", 1},
		{"Write with a worktree", "tools: Read, Write\nisolation: worktree\n", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("---\n" + tc.frontmatter + "---\nBody.\n")

			if got := violations(t, parseAgent(t, data)); len(got) != tc.want {
				t.Errorf("violations = %q, want %d", got, tc.want)
			}
		})
	}
}

// violations checks the rules of docs/arch/0058-probers-run-code-without-committing.md
// and docs/arch/0041-read-only-subagents-name-their-mcp-tools.md.
func violations(t *testing.T, a agent) []string {
	t.Helper()

	tools := toolList(t, a.Tools)
	writes := len(tools) == 0 || slices.Contains(tools, "Edit") || slices.Contains(tools, "Write")

	var found []string

	if writes && a.Isolation != "worktree" {
		found = append(found, "an agent that can edit or write must set isolation: worktree")
	}

	if !writes && slices.Contains(tools, "Bash") && !hasBashHook(a, "read-only-git.sh") {
		found = append(found, "a read-only agent with Bash must run read-only-git.sh as a PreToolUse hook on Bash")
	}

	if !writes {
		for _, tool := range tools {
			if strings.HasPrefix(tool, "mcp__") && !slices.Contains(readOnlyMCPTools, tool) {
				found = append(found, "a read-only agent lists "+tool+", which is not a known read-only MCP tool")
			}
		}
	}

	return found
}

func hasBashHook(a agent, script string) bool {
	for _, matcher := range a.Hooks.PreToolUse {
		if matcher.Matcher != "Bash" {
			continue
		}

		for _, hook := range matcher.Hooks {
			if strings.Contains(hook.Command, ".claude/hooks/"+script) {
				return true
			}
		}
	}

	return false
}

func proberViolations(t *testing.T, a agent) []string {
	t.Helper()

	var found []string

	if a.Isolation != "worktree" {
		found = append(found, "prober must set isolation: worktree")
	}

	if !hasBashHook(a, "prober-bash.sh") {
		found = append(found, "prober must run prober-bash.sh as a PreToolUse hook on Bash")
	}

	tools := toolList(t, a.Tools)
	if !slices.Contains(tools, "Bash") {
		found = append(found, "prober must list Bash")
	}

	for _, tool := range tools {
		if strings.HasPrefix(tool, "mcp__") && !slices.Contains(readOnlyMCPTools, tool) {
			found = append(found, "prober lists "+tool+", which is not a known read-only MCP tool")
		}
	}

	return found
}

// toolList reads a tools field, which Claude Code accepts as a
// comma-separated string or a YAML list.
func toolList(t *testing.T, node yaml.Node) []string {
	t.Helper()

	var entries []string

	switch node.Kind {
	case 0:
		return nil
	case yaml.ScalarNode:
		entries = strings.Split(node.Value, ",")
	case yaml.SequenceNode:
		if err := node.Decode(&entries); err != nil {
			t.Fatalf("decoding tools: %v", err)
		}
	default:
		t.Fatalf("tools field has unexpected YAML kind %d", node.Kind)
	}

	tools := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry = strings.TrimSpace(entry); entry != "" {
			tools = append(tools, entry)
		}
	}

	return tools
}

func parseAgent(t *testing.T, data []byte) agent {
	t.Helper()

	content := string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))

	rest, found := strings.CutPrefix(content, "---\n")
	if !found {
		t.Fatal("no frontmatter")
	}

	frontmatter, _, found := strings.Cut(rest, "\n---\n")
	if !found {
		t.Fatal("frontmatter is not closed")
	}

	var a agent
	if err := yaml.Unmarshal([]byte(frontmatter), &a); err != nil {
		t.Fatalf("parsing frontmatter: %v", err)
	}

	return a
}
