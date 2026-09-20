# ArchGuard (Architectural Drift Detector)

ArchGuard is a CLI tool designed to prevent "architectural drift" by verifying code changes against established Architectural Decision Records (ADRs). It is a semantic compliance engine that uses LLMs (via Ollama) to reason about whether your code changes violate the rules of specific ADRs.

[![Go Report Card](https://goreportcard.com/badge/github.com/tgenz1213/archguard)](https://goreportcard.com/report/github.com/tgenz1213/archguard)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

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

- **Go 1.26+**: [Download Go](https://go.dev/dl/)
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

### 4. Initialize & Run

Set up ArchGuard in your project:

```bash
archguard init
```

This interactive command will:

- Create `archguard.yaml` with defaults
- Set up the ADR directory (default: `./docs/arch`)
- Optionally add an ADR template to get started
- Create `.archguard/` for caching

Then index your ADRs and check for drift:

```bash
archguard index
archguard check --staged
```

---

## 🔒 Privacy & Data Flow

ArchGuard is designed with a "Local First" mentality.

- **Local Analysis**: When using the `ollama` provider, no code or documentation leaves your machine. All embeddings and analysis are performed locally.
- **Cloud Analysis**: When using `openai`, only the relevant code snippets and ADR text required for the specific audit are sent to OpenAI's API.

---

## 🛠️ Configuration

ArchGuard is configured via `archguard.yaml` in the root of your repository.

```yaml
version: "1"

llm:
  provider: "ollama" # or "openai", "gemini"
  model: "llama3.2"
  base_url: "http://localhost:11434"
  max_tokens: 8000
  temperature: 0.0
  # system_prompt: "..." # optional; fully replaces ArchGuard's default judgment
  #   instructions (literal-contradiction, no-inference rules). When set, it is
  #   the only source of judgment behavior sent to the LLM.

vector_store:
  provider: "ollama"
  model: "nomic-embed-text"
  embedding_dim: 768
  similarity_threshold: 0.75 # Global default; an ADR's own frontmatter similarity_threshold overrides this per-ADR
  connection_string: "" # e.g. postgres://user:pass@localhost:5432/archguard
  embedding_concurrency: 5

analysis:
  adr_path: "./docs/arch"
  adr_id_pattern: '^([^-]+)' # Optional: regex that extracts an ADR's ID from its filename; this one is the default (text before the first hyphen)
  frontmatter_mappings: # Optional: read a field from a different frontmatter key; each value shown is the default
    title: "title"
    status: "status"
    scope: "scope" # e.g. "applies_to" if your ADRs use that key
    similarity_threshold: "similarity_threshold"
  pipeline: # Optional: how candidate ADRs are ranked before the LLM judges them; this rank stage matches the default
    rank:
      scorer: "cosine"
      threshold: 0.75 # Minimum score; defaults to vector_store.similarity_threshold
      top_k: 3 # Maximum ADRs kept; defaults to max_relevant_adrs (3 when unset)
      on_error: "skip" # "skip" (default) skips the file when the stage fails; "fail" fails the check
  accepted_statuses: ["Accepted", "Active"] # Use ["*"] to include all statuses
  exclude_patterns:
    - "**/*_test.go"
    - "vendor/**"
    - "go.sum"
    - "README.md"
  
  # Optional Confluence Integration
  confluence:
    enabled: false
    domain: "yourcompany.atlassian.net"
    space_id: "ARCH"
    username: "user@yourcompany.com"
    token: "ATATT3x..."
  max_concurrency: 5 # Number of files analyzed in parallel
```

The optional `analysis` settings above are explained in their own sections below: [Ranking Stages](#ranking-stages), [ADR IDs](#adr-format) (`adr_id_pattern`), and [Frontmatter Field Mappings](#adr-format) (`frontmatter_mappings`).

### Supported Statuses
You can filter ADRs by their status (e.g. `["Accepted"]`). If you want ArchGuard to evaluate against *all* ADRs regardless of status, use `["*"]`.

### Confluence Integration
ArchGuard can natively pull your ADRs from Atlassian Confluence using the Confluence REST API v2. To use it:
1. Enable `confluence` in your `archguard.yaml`.
2. Provide your Confluence `domain` (e.g. `yourcompany.atlassian.net`), the `space_id` where the ADRs live, your `username`, and an API `token`.
3. ArchGuard will crawl the specified space and evaluate your codebase against all pages matching your `accepted_statuses`.

### Ranking Stages

For each changed file, ArchGuard picks which of the ADRs whose `scope` matches it the LLM judges. With no `analysis.pipeline` block it ranks them by cosine similarity, keeps those at or above `vector_store.similarity_threshold`, and judges the top `analysis.max_relevant_adrs` (default 3). `analysis.pipeline` lets you define that ranking as stages instead. Two stages are available, `rank` then `rerank`, both optional, always run in that order and followed by LLM judgment. Each takes:

- `scorer`: how candidates are scored. `cosine` (embedding similarity) is the only scorer today, and it is the default when omitted.
- `threshold`: the minimum score, from 0 to 1. Candidates scoring below it are dropped.
- `top_k`: the maximum number of candidates kept, a positive integer.
- `on_error`: what happens when the stage fails, `skip` or `fail` (see [When a stage fails](#when-a-stage-fails)). Unset behaves as `skip`.

| | `rank` | `rerank` |
|---|---|---|
| Stage omitted | cosine, using `vector_store.similarity_threshold` and `analysis.max_relevant_adrs` | not run |
| `threshold` unset | `vector_store.similarity_threshold` | `0`, with a printed warning |
| `top_k` unset | `analysis.max_relevant_adrs` (3 when unset) | `3`, with a printed warning |

```yaml
analysis:
  pipeline:
    rank:
      scorer: cosine
      threshold: 0.75
      top_k: 5
    rerank:
      threshold: 0.8
      top_k: 2
```

A `rerank` configured without a `rank` runs after the default `rank`. An ADR's own `similarity_threshold` frontmatter overrides a cosine stage's `threshold` for that ADR, so the stage `threshold` is a default for ADRs that don't set their own, not a hard floor.

Every cosine stage re-scores the same candidates, so a cosine `rerank` after a cosine `rank` can only tighten `threshold` or `top_k` and costs one extra embedding call per file. It becomes useful once other scorers can fill a stage.

An unknown scorer, an unrecognized stage or stage key, a non-numeric or out-of-range `threshold`, a non-positive `top_k`, or an `on_error` other than `skip` or `fail` stops `archguard` at startup with exit code 3 and a message naming the stage and key.

#### When a stage fails

By default, if a stage fails for a file (for example the embedding call errors), ArchGuard skips that file, prints the error, counts it in the skipped-files summary, and the run still exits `0`. Set `on_error: fail` on a stage to fail the check instead:

```yaml
analysis:
  pipeline:
    rank:
      on_error: fail
```

Under `fail`, a failed stage stops that file's remaining stages, other files still run, and every failure is printed with its stage, file and error. The run then exits non-zero:

| Failure kind | Meaning | Exit code |
|---|---|---|
| `unavailable` | A dependency did not respond, such as the embedding provider | `6` |
| `precondition_not_met` | The stage could not run at all, such as no embedding provider being configured | `7` |

A run with both kinds exits `7`. `on_error: skip` behaves exactly like leaving it unset. With `--update-baseline`, a `fail` failure exits `6` or `7` without writing the baseline. Under `--format json` the failures are listed in a `failures` array (see [Machine-Readable Output](#machine-readable-output)).

### ADR Format

ArchGuard parses ADRs from Markdown files. Strict **YAML frontmatter** is required.

**Location:** Store your ADRs in the folder specified by `analysis.adr_path` (default `./docs/arch`).

**ADR IDs:** By default, an ADR's ID is derived from its filename by splitting on the first hyphen (`0001-use-postgres.md` → `0001`). If your naming convention doesn't fit that pattern (e.g. `adr-1-use-postgres.md` and `adr-2-use-kafka.md`, which would otherwise both collapse to `adr`), set `analysis.adr_id_pattern` to a regex (for example `adr_id_pattern: '^adr-(\d+)-'`): capture group 1 is used if the pattern defines one, otherwise the whole match is used. A file whose name doesn't match the pattern falls back to the default first-hyphen split, so mixed-convention corpora are handled gracefully. Leave it unset for the default behavior.

**Frontmatter Field Mappings:** If your existing ADR corpus uses different frontmatter key names (e.g. MADR-style or your own house convention), set `analysis.frontmatter_mappings` to remap any of the four canonical fields (`title`, `status`, `scope`, `similarity_threshold`) to the YAML key your files actually use:

```yaml
analysis:
  frontmatter_mappings:
    scope: "applies_to"
```

With this configured, an ADR's `applies_to: "**/*.go"` frontmatter key is read as `scope`. Any field left out of the mapping keeps reading its canonical key unchanged — this is a per-field override, not an all-or-nothing schema replacement. A mapping naming an unknown canonical field, or one that would make two fields read the same YAML key, is rejected at startup.

```markdown
---
title: "No Secrets in Logs"
status: "Accepted"
scope: "**/*.go" # Glob pattern matching file paths to apply this ADR to
similarity_threshold: 0.65 # Optional: overrides vector_store.similarity_threshold for this ADR only
---

## Context

Logging sensitive data is a security risk.

## Decision

Do not print passwords or secrets to console logs.
```

`scope` can also be a YAML list of globs, matched with OR semantics (the ADR applies if *any* pattern matches):

```yaml
scope:
  - "internal/api/**"
  - "internal/handlers/**"
```

**Frontmatter Fields:**

- `title` (Required): Human friendly title.
- `status` (Required): Must match a value in `analysis.accepted_statuses`.
- `scope` (Optional): A glob pattern (e.g., `src/**/*.ts`) or a YAML list of glob patterns matched with OR semantics. Supports standard Go globbing and recursive `**` patterns.
- `similarity_threshold` (Optional): Float overriding the global `vector_store.similarity_threshold` for matching against this ADR only. Falls back to the global value when unset.

### Remote Vector Databases (pgvector)
By default, ArchGuard stores your ADR embeddings in a local `.archguard/index.json` file. For large teams or CI environments, you can centralize this index using PostgreSQL and the `pgvector` extension.

Simply provide a connection string in your `archguard.yaml` or set the `ARCHGUARD_DB_URL` environment variable:

```bash
export ARCHGUARD_DB_URL="postgresql://user:pass@host:5432/db"
archguard index
```

This will automatically create the `archguard_adrs` table and safely scope all ADRs by your repository's Project Name, preventing conflicts across different codebases sharing the same database. ArchGuard automatically manages an HNSW vector graph on this table and uses `ON CONFLICT DO UPDATE` queries to safely maintain the database state without locking table scans.

*Note:* If you're upgrading an existing PgStore install, run `archguard index` once after upgrading. A fix corrected how each ADR's ID and scope are stored in Postgres; existing rows only pick it up on their next index run, and until then `archguard-ignore` suppression, baseline scoping, and the `scope` glob filter may not work correctly for ADRs indexed before the upgrade.

---

## 📖 Usage Guide

### CLI Commands

- `archguard init`: Interactive setup for local development. Creates config, ADR directory, and scaffolding.
- `archguard index`: Parses ADRs and generates vector embeddings. **Run this whenever you add or edit an ADR.** 
  - *Note:* ArchGuard uses **Delta Indexing**, meaning it intelligently skips API calls for ADRs that haven't changed. Feel free to run it frequently!
- `archguard check`: Scans your codebase for violations.
  - `(no arguments)`: Scans uncommitted changes (worktree).
  - `<path>`: Scans a specific file or directory.
  - `--staged`: Scan only staged (index) changes.
  - `--all`: Scan all tracked files.
  - `--debug`: Enable verbose logging.
  - `--ci`: Enable CI-safe mode.
  - `--update-baseline`: Scan the full repository (regardless of other flags/args) and overwrite `archguard-baseline.json` with every currently-detected violation.
  - `--baseline-reason <text>`: With `--update-baseline`, records `<text>` (e.g. `"accepted-debt"` or `"false-positive"`) as the reason on every entry collected this run, applying to all entries rather than just newly baselined ones. Has no effect without `--update-baseline`.
  - `--format <text|json>`: Output format, default `text`. With `--format json`, stdout carries a single JSON document and nothing else (no banner, no progress/debug text — that goes to stderr instead), so it's safe to pipe into another tool. Exit codes are unchanged. Has no effect with `--update-baseline`, which always prints its own text summary.
  - `--suggest-fixes`: For each newly-reported violation, make a second LLM call for a short, unverified remediation pointer (never a guaranteed fix). Off by default — this roughly doubles LLM calls for files with violations.

### Automation & Exit Codes

- **0**: Success (no new violations found; baselined violations still exit 0).
- **1**: General error (e.g. not run inside a git repository, baseline file I/O failure).
- **2**: Usage error (missing/unknown command, bad flags).
- **3**: Config error (failed to load or validate `archguard.yaml`).
- **4**: Architectural drift detected.
- **5**: Index error (failed to build, load, or fetch ADRs for the vector store).
- **6**: A ranking stage with `on_error: fail` could not reach a dependency it needs, such as the embedding provider (see [Ranking Stages](#ranking-stages)).
- **7**: A ranking stage with `on_error: fail` could not run because a precondition was not met, such as no embedding provider being configured. If a run has both kinds of failure, it exits `7`.

Codes `6` and `7` take precedence over `4`: a run that also found drift still exits `6` or `7`, because the check was incomplete. They are only returned when a stage sets `on_error: fail`; by default a failed ranking stage skips its file and the run exits `0` (see [Ranking Stages](#ranking-stages)).

### Machine-Readable Output

`archguard check --format json` prints a single JSON document to stdout (all progress/debug/error text moves to stderr) so it can be piped into another tool:

```json
{
  "violations": [
    {
      "file": "internal/api/handler.go",
      "adr_id": "0003",
      "adr_title": "Repository Pattern for Data Access",
      "line": 42,
      "reasoning": "Handler queries the database directly instead of going through a repository.",
      "quoted_code": "db.Query(\"SELECT * FROM users WHERE id = ?\", id)",
      "suggestion": "Move the query into a repository method and call that from the handler instead."
    }
  ],
  "count": 1,
  "stages": [
    { "name": "rank", "received": 6, "kept": 4, "duration_ms": 812 },
    { "name": "rerank", "received": 4, "kept": 2, "duration_ms": 640 }
  ]
}
```

`stages` lists every stage in the pipeline, in order, including the default `rank` stage when no `analysis.pipeline` is configured. For each stage, `received` and `kept` are the candidate ADRs it was handed and passed on, and `duration_ms` is the total time spent applying the stage (scoring, thresholding and, under `--debug`, writing its debug output), each summed across every file that reaches scoring (a file skipped earlier, such as a truncated file in `--ci` mode, adds nothing). Because files are checked concurrently, `duration_ms` can exceed the run's wall-clock time. A stage that received no candidates reports zeros. Use it to see how much each stage narrows the candidates and what that costs. `archguard check --debug` shows the same per file: the candidates each stage received, the ones it kept with their scores, and the ones it dropped with the reason.

When a stage with `on_error: fail` fails, the document also carries a `failures` array, each entry with the `stage`, the `file`, the `kind` (`unavailable` or `precondition_not_met`) and the underlying `error`; the array is omitted when nothing failed. The error text itself goes to stderr.

`count` matches the number of new (non-baselined) violations that drives the `4` (drift detected) exit code above. `suggestion` is present only when `--suggest-fixes` was passed; it's an LLM-generated pointer, not a verified or guaranteed fix, and it is omitted from the JSON entirely (not an empty string) when `--suggest-fixes` is off or the LLM produced nothing.

> **Note:** run `archguard index` before a `--format json` check. If the index needs an automatic rebuild during `check` (e.g. a stale/missing index, or an ADR provider warning), that rebuild's own progress text currently still prints to stdout ahead of the JSON document (tracked in [#163](https://github.com/Tgenz1213/ArchGuard/issues/163)). With an up-to-date index this doesn't happen.

### Suppression

Intentionally ignore a violation for a specific file using a comment:

```go
// archguard-ignore: 0001
```

- The ignore token must match the **ADR ID** — by default the numeric prefix of the filename, or whatever `analysis.adr_id_pattern` extracts if configured.

### Continuous Integration (CI)

You can run ArchGuard in your CI pipeline to prevent architectural drift from being merged into your main branch.

#### GitHub Actions

ArchGuard is available on the GitHub Marketplace. You can use our official composite action in your `.github/workflows/` files:

```yaml
name: ArchGuard Check
on: [push, pull_request]

jobs:
  archguard:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - name: Run ArchGuard
        uses: Tgenz1213/ArchGuard@main
        with:
          provider: 'ollama'
```

This action automatically sets up Go, installs ArchGuard, and runs `archguard check --ci` on your codebase. If you set `provider: 'ollama'`, it will also automatically install and configure Ollama with the required models.

#### Other CI Providers

If you are not using GitHub Actions, you can run ArchGuard manually by using the `--ci` flag in your pipeline.

**Warn-Open Policy:**
Large files may be truncated to fit the LLM context. In `--ci` mode, truncated files result in a **Warning** rather than a failure, ensuring your pipeline doesn't break due to inconclusive analysis on massive files.

---

## 🔬 Technical Details

- **Semantic Search**: Uses cosine similarity to find relevant ADRs based on the code being analyzed.
- **Index Optimizations**: Employs Delta Indexing to bypass redundant LLM API calls on unchanged files, concurrent provider routines to mask network latency, and conditional HNSW graph maintenance routines in Postgres.
- **Smart Truncation**: Files exceeding the token limit are rolled back to the nearest newline character to preserve code integrity during analysis.
- **Caching**: Analysis results are persisted in `.archguard/cache` based on a hash of the model, ADR content, and file content to reduce API costs and execution time.
- **Baseline Mode**: Run `archguard check --update-baseline` to snapshot every currently-detected violation into `archguard-baseline.json` at the repository root. Subsequent `archguard check` runs load it automatically (no extra flag needed) and treat a matching violation as already-known — excluded from the new-violation count and exit code, but still reported separately (e.g. "3 new violations, 12 baselined"). An entry is invalidated, and its violation re-surfaces as new, once the specific code it originally cited is no longer present in the file. Unlike `.archguard/index.json` and `.archguard/cache/`, `archguard-baseline.json` **is** committed to git, so a team's grandfathered violations travel with the repository. Suppression is per `(ADR, file)` pair, so a second, different violation of an already-baselined ADR in the same file stays hidden until the originally-cited code changes — periodically re-running `--update-baseline` is recommended to catch these.
- **Parallel Execution**: Coordinates analysis across files using a worker pool (defaulting to 5 concurrent workers).

## 🤝 Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for architectural overviews and technical standards.

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.
