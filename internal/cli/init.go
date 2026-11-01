package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type prompter struct{ scanner *bufio.Scanner }

func runInit(configPath string) error {
	in := prompter{scanner: bufio.NewScanner(os.Stdin)}

	existing, err := os.ReadFile(configPath)
	if err == nil {
		return in.addDefaultPipeline(configPath, existing)
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	adrPath, err := in.chooseADRDir()
	if err != nil {
		return err
	}

	if err := writeInitFiles(configPath, adrPath); err != nil {
		return err
	}

	printNextSteps(adrPath, configPath)

	return nil
}

// addDefaultPipeline never overwrites a config: it only replaces analysis.pipeline, and only after asking.
func (p prompter) addDefaultPipeline(configPath string, existing []byte) error {
	splice, err := spliceDefaultPipeline(existing)
	if err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}

	if splice.alreadyList && splice.listErr != nil {
		return fmt.Errorf("%s already has a pipeline list, which init leaves alone, but check rejects it: %w", configPath, splice.listErr)
	}

	if splice.alreadyList {
		fmt.Printf("%s already has a pipeline list; nothing changed.\n", configPath)
		return nil
	}

	replace, err := p.confirm("Replace analysis.pipeline with the default pipeline? (y/n): ")
	if err != nil {
		return err
	}

	if !replace {
		fmt.Println("Initialization cancelled.")
		return nil
	}

	if err := writeInPlace(configPath, splice.result); err != nil {
		return err
	}

	fmt.Printf("Wrote the default pipeline to %s\n", configPath)
	printRemoved(splice.removed)

	if splice.hadPreJudge {
		fmt.Println("The pre_judge screen was removed. To screen with Jev again, add an ask node (see \"Screening with Jev\" in the README).")
	}

	return nil
}

// writeInPlace keeps a symlinked config a link, and keeps the file's mode, which a write-and-rename would replace.
func writeInPlace(configPath string, content []byte) error {
	info, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", configPath, err)
	}

	if err := os.WriteFile(configPath, content, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}

	return nil
}

// printRemoved shows the old pipeline, so settings it tuned can be set again on the new nodes.
func printRemoved(removed []string) {
	if len(removed) == 0 {
		return
	}

	fmt.Println("Removed from the config; set any of these again on the new nodes if you need them:")

	for _, line := range removed {
		fmt.Println("  " + line)
	}
}

func (p prompter) ask(question string) (string, error) {
	fmt.Print(question)
	p.scanner.Scan()

	if err := p.scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read answer: %w", err)
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

func writeInitFiles(configPath, adrPath string) error {
	if err := os.WriteFile(configPath, []byte(generateConfig(adrPath)), 0644); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	fmt.Printf("Created config: %s\n", configPath)

	if err := os.MkdirAll(".archguard/cache", 0755); err != nil {
		return fmt.Errorf("failed to create .archguard directory: %w", err)
	}

	fmt.Println("Created directory: .archguard/cache")

	if err := ensureGitignore(); err != nil {
		return fmt.Errorf("failed to update .gitignore: %w", err)
	}

	return nil
}

func printNextSteps(adrPath, configPath string) {
	configFlag := ""
	if configPath != configFilename {
		configFlag = " --config " + configPath
	}

	fmt.Println("\nArchGuard initialized successfully!")
	fmt.Println("Next steps:")
	fmt.Println("  1. Add your ADR files to", adrPath)
	fmt.Println("  2. Run: archguard index" + configFlag)
	fmt.Println("  3. Run: archguard check" + configFlag)
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
  similarity_threshold: 0.6 # Depends on the embedding model: 0.6 suits nomic-embed-text (see README). An ADR's frontmatter can override it
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
%s`, adrPath, strings.Join(defaultPipelineLines(defaultIndent, defaultIndent), "\n")+"\n")
}

func ensureGitignore() (err error) {
	const (
		gitignorePath  = ".gitignore"
		archguardEntry = ".archguard/"
	)

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
