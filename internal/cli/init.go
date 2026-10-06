package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runInit() error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("Enter ADR directory path [%s]: ", defaultADRPath)
	scanner.Scan()

	if scanner.Err() != nil {
		return fmt.Errorf("input error: %w", scanner.Err())
	}

	adrPath := strings.TrimSpace(scanner.Text())
	if adrPath == "" {
		adrPath = defaultADRPath
	}

	createdDir := false

	if _, err := os.Stat(adrPath); os.IsNotExist(err) {
		fmt.Printf("Directory '%s' does not exist. Create it now? (y/n): ", adrPath)
		scanner.Scan()

		if scanner.Err() != nil {
			return fmt.Errorf("input error: %w", scanner.Err())
		}

		if strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			if err := os.MkdirAll(adrPath, 0755); err != nil {
				return fmt.Errorf("failed to create ADR directory: %w", err)
			}

			fmt.Printf("Created directory: %s\n", adrPath)
			createdDir = true
		} else {
			fmt.Println("Skipping directory creation.")
		}
	}

	if createdDir {
		fmt.Print("Would you like to include a standard ADR_TEMPLATE.md to get started? (y/n): ")
		scanner.Scan()

		if scanner.Err() != nil {
			return fmt.Errorf("input error: %w", scanner.Err())
		}

		if strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			templatePath := filepath.Join(adrPath, "ADR_TEMPLATE.md")
			if err := os.WriteFile(templatePath, []byte(adrTemplateContent), 0644); err != nil {
				return fmt.Errorf("failed to create ADR template: %w", err)
			}

			fmt.Printf("Created template: %s\n", templatePath)
		}
	}

	if _, err := os.Stat(configFilename); err == nil {
		fmt.Printf("%s already exists. Overwrite with defaults? (y/n): ", configFilename)
		scanner.Scan()

		if scanner.Err() != nil {
			return fmt.Errorf("input error: %w", scanner.Err())
		}

		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			fmt.Println("Initialization cancelled.")
			return nil
		}
	}

	configContent := generateConfig(adrPath)
	if err := os.WriteFile(configFilename, []byte(configContent), 0644); err != nil {
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

	fmt.Println("\nArchGuard initialized successfully!")
	fmt.Println("Next steps:")
	fmt.Println("  1. Add your ADR files to", adrPath)
	fmt.Println("  2. Run: archguard index")
	fmt.Println("  3. Run: archguard check")
	return nil
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

func ensureGitignore() error {
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
