package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type prompter struct{ scanner *bufio.Scanner }

func runInit() error {
	in := prompter{scanner: bufio.NewScanner(os.Stdin)}

	adrPath, err := in.chooseADRDir()
	if err != nil {
		return err
	}

	proceed, err := in.confirmConfigOverwrite()
	if err != nil || !proceed {
		return err
	}

	if err := writeInitFiles(adrPath); err != nil {
		return err
	}

	printNextSteps(adrPath)

	return nil
}

func (p prompter) ask(question string) (string, error) {
	fmt.Print(question)
	p.scanner.Scan()

	if err := p.scanner.Err(); err != nil {
		return "", fmt.Errorf("input error: %w", err)
	}

	return strings.TrimSpace(p.scanner.Text()), nil
}

func (p prompter) confirm(question string) (bool, error) {
	answer, err := p.ask(question)
	if err != nil {
		return false, err
	}

	return strings.ToLower(answer) == "y", nil
}

func (p prompter) chooseADRDir() (string, error) {
	adrPath, err := p.ask(fmt.Sprintf("Enter ADR directory path [%s]: ", defaultADRPath))
	if err != nil {
		return "", err
	}

	if adrPath == "" {
		adrPath = defaultADRPath
	}

	if _, err := os.Stat(adrPath); !os.IsNotExist(err) {
		return adrPath, nil
	}

	created, err := p.createADRDir(adrPath)
	if err != nil {
		return "", err
	}

	if created {
		if err := p.offerTemplate(adrPath); err != nil {
			return "", err
		}
	}

	return adrPath, nil
}

func (p prompter) createADRDir(adrPath string) (bool, error) {
	create, err := p.confirm(fmt.Sprintf("Directory '%s' does not exist. Create it now? (y/n): ", adrPath))
	if err != nil {
		return false, err
	}

	if !create {
		fmt.Println("Skipping directory creation.")
		return false, nil
	}

	if err := os.MkdirAll(adrPath, 0755); err != nil {
		return false, fmt.Errorf("failed to create ADR directory: %w", err)
	}

	fmt.Printf("Created directory: %s\n", adrPath)

	return true, nil
}

func (p prompter) offerTemplate(adrPath string) error {
	include, err := p.confirm("Would you like to include a standard ADR_TEMPLATE.md to get started? (y/n): ")
	if err != nil || !include {
		return err
	}

	templatePath := filepath.Join(adrPath, "ADR_TEMPLATE.md")
	if err := os.WriteFile(templatePath, []byte(adrTemplateContent), 0644); err != nil {
		return fmt.Errorf("failed to create ADR template: %w", err)
	}

	fmt.Printf("Created template: %s\n", templatePath)

	return nil
}

func (p prompter) confirmConfigOverwrite() (bool, error) {
	if _, err := os.Stat(configFilename); err != nil {
		return true, nil
	}

	overwrite, err := p.confirm(fmt.Sprintf("%s already exists. Overwrite with defaults? (y/n): ", configFilename))
	if err != nil {
		return false, err
	}

	if !overwrite {
		fmt.Println("Initialization cancelled.")
		return false, nil
	}

	return true, nil
}

func writeInitFiles(adrPath string) error {
	if err := os.WriteFile(configFilename, []byte(generateConfig(adrPath)), 0644); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	fmt.Printf("Created config: %s\n", configFilename)

	if err := os.MkdirAll(".archguard/cache", 0755); err != nil {
		return fmt.Errorf("failed to create .archguard directory: %w", err)
	}

	fmt.Println("Created directory: .archguard/cache")

	if err := ensureGitignore(); err != nil {
		return fmt.Errorf("failed to update .gitignore: %w", err)
	}

	return nil
}

func printNextSteps(adrPath string) {
	fmt.Println("\nArchGuard initialized successfully!")
	fmt.Println("Next steps:")
	fmt.Println("  1. Add your ADR files to", adrPath)
	fmt.Println("  2. Run: archguard index")
	fmt.Println("  3. Run: archguard check")
}

func generateConfig(adrPath string) string {
	return fmt.Sprintf(`version: "1"

llm:
  provider: "ollama"
  model: "llama3.2"
  base_url: "http://localhost:11434"
  max_tokens: 8000
  temperature: 0.0

vector_store:
  provider: "ollama"
  model: "nomic-embed-text"
  embedding_dim: 768
  similarity_threshold: 0.75 # Global default; an ADR's own frontmatter similarity_threshold overrides this per-ADR
  connection_string: ""
  embedding_concurrency: 5

analysis:
  adr_path: "%s"
  accepted_statuses: ["Accepted", "Active"]
  exclude_patterns:
    - "**/*_test.go"
    - "vendor/**"
    - "go.sum"
    - "README.md"
    - "bin/**"
`, adrPath)
}

func ensureGitignore() (err error) {
	const gitignorePath = ".gitignore"
	const archguardEntry = ".archguard/"

	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == archguardEntry {
			return nil
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}()

	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	if _, err := f.WriteString(archguardEntry + "\n"); err != nil {
		return err
	}

	fmt.Printf("Added %s to .gitignore\n", archguardEntry)
	return nil
}

const adrTemplateContent = `---
title: "[Short, Descriptive Title]"
status: "[Accepted | Proposed | Superseded]"
scope: "[Optional: glob pattern, e.g., **/*.go -- or a YAML list of globs, matched with OR semantics]"
---

# [ADR Title]

## Context

[Describe the problem or context that requires a decision.]

## Decision

[Clearly state the decision and any rules or constraints it imposes.]

## Consequences

[Describe the expected outcomes, both positive and negative.]
`
