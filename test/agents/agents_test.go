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

type hookCommand struct {
	Command string `yaml:"command"`
}

type hookMatcher struct {
	Matcher string        `yaml:"matcher"`
	Hooks   []hookCommand `yaml:"hooks"`
}

type agent struct {
	Tools           yaml.Node `yaml:"tools"`
	DisallowedTools yaml.Node `yaml:"disallowedTools"`
	Isolation       string    `yaml:"isolation"`
	Hooks           struct {
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
		{"reviewer", "tools: Read, Grep, Glob, Bash\ndisallowedTools: mcp__*\n" + readOnlyHook, 0},
		{"reviewer as a YAML list", "tools: [Read, Bash]\ndisallowedTools: [mcp__*]\n" + readOnlyHook, 0},
		{"reviewer without disallowedTools", "tools: Read, Grep, Glob, Bash\n" + readOnlyHook, 1},
		{"reviewer denying one server only", "tools: Read, Bash\ndisallowedTools: mcp__serena\n" + readOnlyHook, 1},
		{"reviewer without the hook", "tools: Read, Grep, Glob, Bash\ndisallowedTools: mcp__*\n", 1},
		{"hook on another matcher", "tools: Read, Bash\ndisallowedTools: mcp__*\n" + strings.Replace(readOnlyHook, "matcher: Bash", "matcher: Read", 1), 1},
		{"read-only without Bash", "tools: Read, Grep\ndisallowedTools: mcp__*\n", 0},
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

// violations checks the rules of docs/arch/0040-isolated-subagents.md and
// docs/arch/0041-read-only-subagents-get-no-mcp-tools.md.
func violations(t *testing.T, a agent) []string {
	t.Helper()

	tools := toolList(t, a.Tools)
	writes := len(tools) == 0 || slices.Contains(tools, "Edit") || slices.Contains(tools, "Write")

	var found []string

	if writes && a.Isolation != "worktree" {
		found = append(found, "an agent that can edit or write must set isolation: worktree")
	}

	if !writes && slices.Contains(tools, "Bash") && !hasReadOnlyGitHook(a) {
		found = append(found, "a read-only agent with Bash must run read-only-git.sh as a PreToolUse hook on Bash")
	}

	if !writes && !slices.Contains(toolList(t, a.DisallowedTools), "mcp__*") {
		found = append(found, "a read-only agent must set disallowedTools: mcp__*")
	}

	return found
}

func hasReadOnlyGitHook(a agent) bool {
	for _, matcher := range a.Hooks.PreToolUse {
		if matcher.Matcher != "Bash" {
			continue
		}

		for _, hook := range matcher.Hooks {
			if strings.Contains(hook.Command, ".claude/hooks/read-only-git.sh") {
				return true
			}
		}
	}

	return false
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
