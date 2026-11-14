package review

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const rolesDir = "../../.claude/review/roles"

var roleStages = map[string]string{
	"standards":                 "reviewer",
	"breaker":                   "reviewer",
	"ticket-and-necessity":      "reviewer",
	"reinvention":               "reviewer",
	"simplicity-and-repetition": "reviewer",
	"tests":                     "reviewer",
	"contract-and-blast-radius": "reviewer",
	"reproducer":                "verifier",
	"guard-hunter":              "verifier",
	"provenance":                "verifier",
	"impact":                    "verifier",
}

var verifierInputs = []string{"claim", "file_line", "evidence", "repro", "diff", "ticket", "rules"}

// inputsOnlyFor names the inputs that would unblind any verifier but the one listed.
var inputsOnlyFor = map[string]string{
	"ticket": "provenance",
	"rules":  "guard-hunter",
}

type role struct {
	Name   string   `yaml:"name"`
	Stage  string   `yaml:"stage"`
	Agent  string   `yaml:"agent"`
	Inputs []string `yaml:"inputs"`
	Body   string   `yaml:"-"`
}

func TestRoleFilesAreWellFormed(t *testing.T) {
	roles := readRoles(t)

	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, r.Name)

		if want, ok := roleStages[r.Name]; !ok {
			t.Errorf("%s: not a known role", r.Name)
		} else if r.Stage != want {
			t.Errorf("%s: stage %q, want %q", r.Name, r.Stage, want)
		}

		if r.Agent != "reviewer" && r.Agent != "prober" {
			t.Errorf("%s: agent %q, want reviewer or prober", r.Name, r.Agent)
		}

		if strings.TrimSpace(r.Body) == "" {
			t.Errorf("%s: empty prompt", r.Name)
		}
	}

	for name := range roleStages {
		if !slices.Contains(names, name) {
			t.Errorf("no role file for %s", name)
		}
	}
}

func TestVerifierInputsAreBlind(t *testing.T) {
	for _, r := range readRoles(t) {
		if r.Stage != "verifier" {
			if len(r.Inputs) > 0 {
				t.Errorf("%s: a reviewer role lists inputs", r.Name)
			}

			continue
		}

		if len(r.Inputs) == 0 {
			t.Errorf("%s: no inputs", r.Name)
		}

		for _, input := range r.Inputs {
			if !slices.Contains(verifierInputs, input) {
				t.Errorf("%s: input %q is not one of %v", r.Name, input, verifierInputs)
			}

			if owner, ok := inputsOnlyFor[input]; ok && owner != r.Name {
				t.Errorf("%s: input %q is only for %s", r.Name, input, owner)
			}
		}
	}
}

func readRoles(t *testing.T) []role {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(rolesDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}

	if len(files) == 0 {
		t.Fatal("no role files found")
	}

	roles := make([]role, 0, len(files))
	for _, file := range files {
		r := parseRole(t, file)

		if stem := strings.TrimSuffix(filepath.Base(file), ".md"); r.Name != stem {
			t.Errorf("%s: name %q does not match the file name", file, r.Name)
		}

		roles = append(roles, r)
	}

	return roles
}

func parseRole(t *testing.T, file string) role {
	t.Helper()

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	content := strings.ReplaceAll(string(data), "\r", "")

	rest, found := strings.CutPrefix(content, "---\n")
	if !found {
		t.Fatalf("%s: no frontmatter", file)
	}

	frontmatter, body, found := strings.Cut(rest, "\n---\n")
	if !found {
		t.Fatalf("%s: frontmatter is not closed", file)
	}

	var r role
	if err := yaml.Unmarshal([]byte(frontmatter), &r); err != nil {
		t.Fatalf("%s: parsing frontmatter: %v", file, err)
	}

	r.Body = body

	return r
}
