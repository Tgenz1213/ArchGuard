# ArchGuard (Architectural Drift Detector)

ArchGuard is a CLI tool designed to prevent "architectural drift" by verifying code changes against established Architectural Decision Records (ADRs). It is a semantic compliance engine that uses LLMs (via Ollama) to reason about whether your code changes violate the rules of specific ADRs.

## 🔍 See it in Action

ArchGuard sits between your code and your commit. When it detects code that violates your Architectural Decision Records (ADRs), it alerts you before the drift merges.

```text
$ archguard check --staged
Analyzing internal/db/conn.js...
  Checking against ADR: Use Golang for Backend Services (0.92)

  [VIOLATION] Use Golang for Backend Services [Line 1]
  Reasoning: The file uses '.js' extension and contains JavaScript code, which violates the mandatory requirement to use Go for all backend logic.
  Code: const express = require('express');
```

## ⚡ Quick Start

### 1. Prerequisites

- **Go 1.25+**: [Download Go](https://go.dev/dl/)
- **Ollama**: [Download Ollama](https://ollama.com/)

### 2. Setup Models

Start Ollama and pull the required models:

```bash
ollama serve
# In a new terminal:
ollama pull llama3.2
ollama pull nomic-embed-text
```

### 3. Install

#### Quick Install

```bash
go install github.com/tgenz1213/archguard/cmd/archguard@latest
```

#### Build

```bash
git clone https://github.com/tgenz1213/archguard.git
cd archguard
go install ./cmd/archguard
```

### 4. Configure

Create a minimal `archguard.yaml` in your project root:

```yaml
version: "1"
llm:
  provider: "ollama"
  model: "llama3.2"
vector_store:
  provider: "ollama"
  model: "nomic-embed-text"
analysis:
  adr_path: "./docs/arch" # Folder containing your ADR markdown files
  accepted_statuses: ["Accepted"]
```

### 5. Run

Index your ADRs and check for drift:

```bash
./archguard index
./archguard check --staged
```

---

## 🛠️ Configuration

### archguard.yaml Reference

The configuration file controls which models are used and how files are scanned.

```yaml
version: "1"

llm:
  provider: "ollama"
  model: "llama3.2"
  base_url: "http://localhost:11434"
  max_tokens: 8000
  temperature: 0.0
  system_prompt: "You are a custom AI auditor..." # Optional: Override the default system prompt

vector_store:
  provider: "ollama"
  model: "nomic-embed-text"
  embedding_dim: 768
  similarity_threshold: 0.75 # Minimum 0-1 score to trigger an LLM check

analysis:
  adr_path: "./docs/arch"
  accepted_statuses: ["Accepted", "Active"]
  exclude_patterns:
    - "**/*.test.go"
    - "vendor/**"
    - "go.sum"
```

### ADR Format

ArchGuard parses ADRs from Markdown files. Strict **YAML frontmatter** is required.

**Location:** Store your ADRs in the folder specified by `analysis.adr_path` (default `./docs/arch`).

```markdown
---
title: "No Secrets in Logs"
status: "Accepted"
scope: "**/*.go" # Glob pattern matching file paths to apply this ADR to
---

## Context

Logging sensitive data is a security risk.

## Decision

Do not print passwords or secrets to console logs.
```

**Frontmatter Fields:**

- `title` (Required): Human friendly title.
- `status` (Required): Must match a value in `analysis.accepted_statuses`.
- `scope` (Optional): Glob pattern (e.g., `src/**/*.ts`, `**/*_test.go`). If omitted, it applies to all files.

## 📖 Usage Guide

### CLI Commands

- `archguard index`: Parses ADRs and generates vector embeddings. **Run this whenever you add or edit an ADR.**
- `archguard check`: Scans your codebase for violations.
  - `(no arguments)`: Scans uncommitted changes (worktree).
  - `<path>`: Scans a specific file or directory.
  - `--staged`: Scan only staged (index) changes. (Recommended for pre-commit hooks)
  - `--all`: Scan all tracked files.
  - `--debug`: Enable verbose logging.
  - `--ci`: Enable CI-safe mode.

### Suppression

Intentionally ignore a violation for a specific file using a comment:

```go
// archguard-ignore: 001-no-secrets
func printSecret() {
    fmt.Println(secret)
}
```

- The ignore token must match the **ADR file name** (or ID prefix) exactly.

### Continuous Integration (CI)

Use the `--ci` flag in your pipeline.

```bash
./archguard check --ci --staged
```

**Warn-Open Policy:**
Large files may be truncated to fit the LLM context. In `--ci` mode, truncated files result in a **Warning** rather than a failure, ensuring your pipeline doesn't break due to inconclusive analysis on massive files.

## ❓ Troubleshooting

**"connection refused" or LLM errors:**
Ensure Ollama is running in the background:

```bash
ollama serve
```

**"No relevant ADRs found" despite obvious violations:**

1.  Check your `similarity_threshold` in `archguard.yaml`. If it's too high (e.g., 0.95), lower it to 0.75.
2.  Ensure you ran `./archguard index` after creating the ADR.

## 🤝 Contributing

Contributions are welcome!

1.  Fork the Project
2.  Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3.  Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4.  Push to the Branch (`git push origin feature/AmazingFeature`)
5.  Open a Pull Request

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.
