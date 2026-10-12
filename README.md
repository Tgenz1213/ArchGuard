# ArchGuard (Architectural Drift Detector)

ArchGuard is a CLI tool designed to prevent "architectural drift" by verifying code changes against established Architectural Decision Records (ADRs). It is a semantic compliance engine that uses LLMs (via Ollama) to reason about whether your code changes violate the rules of specific ADRs.

[![Go Report Card](https://goreportcard.com/badge/github.com/tgenz1213/archguard)](https://goreportcard.com/report/github.com/tgenz1213/archguard)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 🔍 See it in Action

ArchGuard sits between your code and your commit. When it detects code that violates your Architectural Decision Records (ADRs), it alerts you before the drift merges.

```text
$ archguard check --staged
internal/db/conn.js
  [VIOLATION] Use Golang for Backend Services [Line 1]
    Reasoning: The file uses '.js' extension and contains JavaScript code, which violates the mandatory requirement to use Go for all backend logic.
    Code: const express = require('express');
```

## ⚡ Quick Start

### 1. Prerequisites

- **Go**: the version [`go.mod`](go.mod) declares. [Download Go](https://go.dev/dl/)
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

ArchGuard is configured via `archguard.yaml` in the root of your repository, or the file `check` and `index` are given with `--config`.

```yaml
version: "1"

llm:
  provider: "ollama" # or "openai", "gemini"
  model: "llama3.2"
  base_url: "http://localhost:11434"
  max_tokens: 8000
  temperature: 0.0 # Optional; ollama, openai, gemini and claude all send it, unset sends nothing. Omit it for models that reject it (e.g. OpenAI reasoning models).
  # system_prompt: "..." # optional; fully replaces ArchGuard's default judgment
  #   instructions (literal-contradiction, no-inference rules). When set, it is
  #   the only source of judgment behavior sent to the LLM.
  # cost_per_1k_input_tokens: 0.003 # Optional, with cost_per_1k_output_tokens; see Token Usage and Cost
  # cost_per_1k_output_tokens: 0.015

vector_store:
  provider: "ollama"
  model: "nomic-embed-text"
  embedding_dim: 768
  similarity_threshold: 0.6 # Depends on the embedding model: 0.6 suits nomic-embed-text (see Choosing similarity_threshold below). An ADR's frontmatter can override it
  connection_string: "" # e.g. postgres://user:pass@localhost:5432/archguard
  embedding_concurrency: 5
  # cost_per_1k_tokens: 0.00002 # Optional; see Token Usage and Cost

analysis:
  adr_path: "./docs/arch"
  adr_id_pattern: '^([^-]+)' # Optional: regex that extracts an ADR's ID from its filename; this one is the default (text before the first hyphen)
  frontmatter_mappings: # Optional: read a field from a different frontmatter key; each value shown is the default
    title: "title"
    status: "status"
    scope: "scope" # e.g. "applies_to" if your ADRs use that key
    similarity_threshold: "similarity_threshold"
    rules: "rules"
  rules_heading: "Rules" # Optional: the ADR body section whose bullets are read as screening rules; this is the default
  # included_headers: ["Decision", "Rules"] # Optional: embed only these ADR sections; see Embedded Sections
  # excluded_headers: ["Context"] # Optional: embed every ADR section but these; ignored when included_headers is set
  pipeline: # Optional: how candidate ADRs are ranked before the LLM judges them; this rank stage matches the default
    rank:
      scorer: "cosine"
      threshold: 0.6 # Minimum score; defaults to vector_store.similarity_threshold
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

The optional `analysis` settings above are explained in their own sections below: [Ranking Stages](#ranking-stages), [ADR IDs](#adr-format) (`adr_id_pattern`), [Frontmatter Field Mappings](#adr-format) (`frontmatter_mappings`), [Screening Rules](#screening-rules) (`rules_heading`), and [Embedded Sections](#embedded-sections) (`included_headers`, `excluded_headers`).

### Supported Statuses
You can filter ADRs by their status (e.g. `["Accepted"]`). If you want ArchGuard to evaluate against *all* ADRs regardless of status, use `["*"]`.

### Confluence Integration
ArchGuard can natively pull your ADRs from Atlassian Confluence using the Confluence REST API v2. To use it:
1. Enable `confluence` in your `archguard.yaml`.
2. Provide your Confluence `domain` (e.g. `yourcompany.atlassian.net`), the `space_id` where the ADRs live, your `username`, and an API `token`.
3. ArchGuard will crawl the specified space and evaluate your codebase against all pages matching your `accepted_statuses`.

### Choosing `similarity_threshold`

`vector_store.similarity_threshold` is the lowest cosine similarity an ADR can score against a changed file and still be judged. Scores depend on the embedding model, so a value that works for one model can drop almost every ADR with another. Set it for the model you use.

Measured on this repository: 30 ADRs against the files changed in 60 commits, with 16 of those changes labeled by hand with the ADRs that apply ([#295](https://github.com/Tgenz1213/ArchGuard/issues/295), [#311](https://github.com/Tgenz1213/ArchGuard/issues/311)):

| Embedding model | Best ADR score per Go file (10th to 90th percentile) | Start at |
|---|---|---|
| Ollama `nomic-embed-text` | 0.69 to 0.79 | `0.6` |
| Gemini `gemini-embedding-2` | 0.68 to 0.81 | `0.55` |
| OpenAI `text-embedding-3-small` | 0.44 to 0.69 | `0.3` |
| Voyage `voyage-4` | 0.56 to 0.76 | `0.4` |

At those values nearly every Go file reaches the model and `analysis.max_relevant_adrs` does the selecting. At `0.75`, 46% (Gemini) to 99% (OpenAI) of the Go files had no ADR above the threshold, and at most 37% of the ADRs that applied were judged. Raising `max_relevant_adrs` from 3 to 6 judged more of the ADRs that applied (nomic: 67% to 85%; Gemini: 81% to 93%; OpenAI: 78% to 81%; Voyage: 78% to 93%), at up to twice the LLM calls. Other models, such as `text-embedding-3-large`, have not been measured; find a value as below.

To find a value, run `archguard check --debug` on a few typical changes. Each file lists the ADRs it kept and those `Below threshold`, with their scores. Set the threshold just below the scores of the ADRs you expect to apply, and let `max_relevant_adrs` bound the cost.

- **Too high:** the report's coverage line counts files whose candidates all scored below the threshold, or the run says no file was checked against an ADR.
- **Too low:** nearly every file is judged against `max_relevant_adrs` ADRs, including ADRs unrelated to the change, so a run makes more LLM calls for little gain.

An ADR can set its own `similarity_threshold` in frontmatter (see [ADR Format](#adr-format)).

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
      threshold: 0.6
      top_k: 5
    rerank:
      threshold: 0.65
      top_k: 2
```

A `rerank` configured without a `rank` runs after the default `rank`. An ADR's own `similarity_threshold` frontmatter overrides a cosine stage's `threshold` for that ADR, so the stage `threshold` is a default for ADRs that don't set their own, not a hard floor.

Every cosine stage re-scores the same candidates, so a cosine `rerank` after a cosine `rank` can only tighten `threshold` or `top_k` and costs one extra embedding call per file. It becomes useful once other scorers can fill a stage.

An unknown scorer, an unrecognized stage or stage key, a non-numeric or out-of-range `threshold`, a non-positive `top_k`, or an `on_error` other than `skip` or `fail` stops `archguard` at startup with exit code 3 and a message naming the stage and key.

#### When a stage fails

By default, if a stage fails for a file (for example the embedding call errors), ArchGuard skips that file, prints the error, lists it under the report's skipped files, and the run still exits `0`, unless no ADR check returned a verdict in the whole run, which exits `8`. Set `on_error: fail` on a stage to fail the check instead:

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

A run with both kinds exits `7`. `on_error: skip` behaves exactly like leaving it unset. With `--update-baseline`, a `fail` failure exits `6` or `7`, and a run left with no ADR check exits `8`, without writing the baseline. Under `--format json` the failures are listed in a `failures` array (see [Machine-Readable Output](#machine-readable-output)).

### ADR Format

ArchGuard parses ADRs from Markdown files. Strict **YAML frontmatter** is required.

**Location:** Store your ADRs in the folder specified by `analysis.adr_path` (default `./docs/arch`).

**ADR IDs:** By default, an ADR's ID is derived from its filename by splitting on the first hyphen (`0001-use-postgres.md` → `0001`). If your naming convention doesn't fit that pattern (e.g. `adr-1-use-postgres.md` and `adr-2-use-kafka.md`, which would otherwise both collapse to `adr`), set `analysis.adr_id_pattern` to a regex (for example `adr_id_pattern: '^adr-(\d+)-'`): capture group 1 is used if the pattern defines one, otherwise the whole match is used. A pattern with more than one capture group is a config error (exit `3`), because it is unclear which group is the ID; use `(?:...)` for groups that are not the ID. A file whose name doesn't match the pattern falls back to the default first-hyphen split, so mixed-convention corpora are handled gracefully. If the pattern matches none of the ADR files, `index` and `check` print a warning (on stderr for `check`), since every ID then comes from the fallback. Leave it unset for the default behavior.

**Frontmatter Field Mappings:** If your existing ADR corpus uses different frontmatter key names (e.g. MADR-style or your own house convention), set `analysis.frontmatter_mappings` to remap any of the five canonical fields (`title`, `status`, `scope`, `similarity_threshold`, `rules`) to the YAML key your files actually use:

```yaml
analysis:
  frontmatter_mappings:
    scope: "applies_to"
```

With this configured, an ADR's `applies_to: "**/*.go"` frontmatter key is read as `scope`. Any field left out of the mapping keeps reading its canonical key unchanged — this is a per-field override, not an all-or-nothing schema replacement. A mapping naming an unknown canonical field, or one that would make two fields read the same YAML key, is rejected at startup. Because `rules` reads the `rules` key by default, mapping another field onto `rules` is rejected too; the error tells you to remap `rules` itself (for example `rules: rules_field`).

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
- `rules` (Optional): Screening rules stating what a violation of this ADR looks like. See [Screening Rules](#screening-rules).

#### Screening Rules

An ADR can state, as short rules, exactly what violating code looks like. ArchGuard loads and indexes these rules for violation-screening scorers; they don't yet change which ADRs `check` judges or what it reports. The usual place is a `## Rules` section in the body, where each bullet is one rule:

```markdown
## Rules

- All code MUST be written in Go.
- There MUST NOT be hand rolled logic where a well-tested library exists to solve the same problem
```

The section is found by its heading text (`Rules` by default, set `analysis.rules_heading` to use another), matched at any heading level, ignoring case, inline formatting and a trailing colon (so `## **Rules:**` matches), and it ends at the next heading of the same or a higher level. Only the top-level bullets are rules, with any task-list checkbox (`[ ]`, `[x]`) dropped; nested bullets and prose around the list are ignored, so a rule can carry its own explanation underneath.

Rules can also go in the frontmatter, as a list of statements. A rule written as a mapping can add examples of violating and compliant code:

```yaml
rules:
  - All code MUST be written in Go.
  - statement: Handlers must not import the database package directly
    violating:
      - 'import "app/db"'
    compliant:
      - call the repository interface instead
```

If an ADR declares rules in both places, only the frontmatter rules are used. An ADR with malformed rules (for example `rules: yes`, or a bullet in the rules section that has no text of its own) still loads and is still judged, just without its rules: `archguard index` and `archguard check` print a warning, and the `archguard index` summary lists it under `Rules ignored (malformed)` with the reason. Rules are stored in the index alongside each ADR, so editing only an ADR's frontmatter rules is picked up by the next `archguard index`, and with the local index by the next `archguard check` too.

### Embedded Sections

By default ArchGuard embeds each ADR's whole body, so narrative sections such as Context take part in deciding which ADRs a changed file is matched against. `analysis.included_headers` and `analysis.excluded_headers` choose which `##` sections are embedded instead:

```yaml
analysis:
  included_headers: # embed only these sections
    - Decision
    - Rules
  # excluded_headers: # or embed every section but these
  #   - Context
```

- A section runs from its `##` heading to the next `#` or `##` heading, so it includes any `###` beneath it. Headings are matched ignoring case, inline formatting and a trailing colon, like `rules_heading`.
- With `included_headers`, text before the first `##` heading is left out; with `excluded_headers` it is kept. Each ADR's title and status are always embedded.
- When both are set, `included_headers` applies and a warning says `excluded_headers` is ignored.
- Only the embedding changes: the LLM still judges each file against the full ADR.
- An ADR with no text left to embed is not indexed, and the `archguard index` summary lists it under `Failed to embed or persist`.
- Changing either setting embeds every ADR again on the next `archguard index`, and with the local index on the next `archguard check` too.

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
  - `--color <auto|always|never>`: Same as `check`'s `--color`.
  - `--config <path>`: Same as `check`'s `--config`.
  - *Note:* ArchGuard uses **Delta Indexing**: only new or edited ADRs are embedded, and the rest keep their saved embeddings unless `vector_store.model` or `vector_store.embedding_dim` changed, or a new ArchGuard version changed how embeddings are built. In that last case the next `archguard index` embeds every ADR again; with a local index, `archguard check` does it too. Feel free to run it frequently!
- `archguard check`: Scans your codebase for violations.
  - `(no arguments)`: Scans uncommitted changes (worktree).
  - `<path>`: Scans a specific file or directory.
  - `--staged`: Scan only staged (index) changes.
  - `--all`: Scan all tracked files.
  - `--since <ref>`: Scan the files that differ between `<ref>` and `HEAD`, judging only the lines the change added or removed; a finding that quotes only code the change left alone is dropped (see [ADR 0030](docs/arch/0030-judge-the-change-under-since.md)). For a pull request, use its base commit; if `<ref>` has moved on since the branch started, use `$(git merge-base <ref> HEAD)`. Cannot be combined with paths, `--staged`, `--all` or `--update-baseline`.
  - `--whole-file`: With `--since`, judge each changed file in full instead of only the lines the change touched, so touching a file means leaving it compliant: a violation on a line the change did not touch is reported too. Needs `--since`.
  - `--debug`: Enable verbose logging.
  - `--ci`: Enable CI-safe mode. It needs a scope (`--since`, `--all`, `--staged` or paths): a CI checkout has no uncommitted changes, so a bare `--ci` would check nothing and is rejected.
  - `--update-baseline`: Scan the full repository (regardless of other flags/args) and overwrite `archguard-baseline.json` with every currently-detected violation. The run ends with a report of what it recorded (see [Reading a Check Run](#reading-a-check-run)).
  - `--baseline-reason <text>`: With `--update-baseline`, records `<text>` (e.g. `"accepted-debt"` or `"false-positive"`) as the reason on every entry collected this run, applying to all entries rather than just newly baselined ones. Has no effect without `--update-baseline`.
  - `--format <text|json>`: Output format, default `text`. With `--format json`, stdout carries a single JSON document and nothing else (no banner, no progress/debug text — that goes to stderr instead), so it's safe to pipe into another tool. Exit codes are unchanged. Has no effect with `--update-baseline`, which always prints its baseline report as text.
  - `--output <path>`: Write the report (text or JSON, per `--format`) to `<path>` instead of stdout. The file has no color, stdout stays empty, and the log says where the report was written. A relative path is taken from the directory you ran the command in. The directory must already exist; a missing directory, a path that is itself a directory, or the baseline file (`archguard-baseline.json`) exits `1` before any analysis runs. Also works with `--update-baseline`.
  - `--config <path>`: Read the config from `<path>` instead of `archguard.yaml` at the repository root, for example a CI-only config. A relative path is taken from the directory you ran the command in. A missing or unreadable file exits `3` and names the path.
  - `--color <auto|always|never>`: When to color output, default `auto`. `auto` colors only a stream that is a terminal, and turns color off when `NO_COLOR` is set to a non-empty value, `TERM=dumb`, or `CI` is set (so CI logs stay plain; pass `--color=always` to color them). `always` and `never` override all of that. `--format json`'s stdout is never colored.
  - `--suggest-fixes`: For each newly-reported violation, make a second LLM call for a short, unverified remediation pointer (never a guaranteed fix). Off by default — this roughly doubles LLM calls for files with violations.
  - `--dry-run`: Preview what a check would cost. Everything up to the judgment calls runs, embeddings included, then each judgment is counted instead of sent (see [Token Usage and Cost](#token-usage-and-cost)). Reports no violations and exits `0` unless an error stops the run (a config, index, git or report-write error). Cannot be combined with `--update-baseline`.

### Automation & Exit Codes

- **0**: Success (no new violations found; baselined violations and unverified findings still exit 0). A `--dry-run` exits 0 unless an error stops it.
- **1**: General error (e.g. not run inside a git repository, baseline file I/O failure), or the command's primary output (results, help, the JSON document) could not be written, e.g. a closed stdout. This takes precedence over every other code except an interrupt.
- **2**: Usage error (missing/unknown command, bad flags).
- **3**: Config error (failed to load or validate `archguard.yaml`).
- **4**: Architectural drift detected. The report is the result, so no `Error:` line repeats it.
- **5**: Index error (failed to build, load, or fetch ADRs for the vector store).
- **6**: A ranking stage with `on_error: fail` could not reach a dependency it needs, such as the embedding provider (see [Ranking Stages](#ranking-stages)).
- **7**: A ranking stage with `on_error: fail` could not run because a precondition was not met, such as no embedding provider being configured. If a run has both kinds of failure, it exits `7`.
- **8**: Errors left the run with no ADR check: files were skipped or ADR checks failed (for example the database, the embedding provider or the LLM was unreachable) and no ADR check returned a verdict. The report still lists what failed. A run where at least one ADR check returned a verdict exits as usual, with the failures listed in the report. A file skipped as too large under `--ci` does not count. See [ADR 0032](docs/arch/0032-a-run-that-checked-nothing-fails.md).
- **130**: Interrupted by SIGINT or SIGTERM (Ctrl-C, or a cancelled or timed-out CI job). In-flight git and LLM calls are cancelled, no report is printed, and `--update-baseline` leaves the existing baseline file untouched. A second signal kills the process immediately; CI runners typically send SIGINT, then SIGTERM a few seconds later, which is that second signal.

Codes `6` and `7` take precedence over `4`: a run that also found drift still exits `6` or `7`, because the check was incomplete. They are only returned when a stage sets `on_error: fail`; by default a failed ranking stage skips its file, and the run exits `0` unless that left it with no ADR check (`8`) (see [Ranking Stages](#ranking-stages)). Codes `6` and `7` also take precedence over `8`.

### Reading a Check Run

In text mode a `check` run produces two outputs. **stdout carries the report**, printed once at the end of the run. **stderr carries the log**: the banner, progress, warnings, and one line per finding as it happens. That lets you keep the report without the noise:

```bash
archguard check --all > report.txt
```

The report lists every new violation grouped by file, then any unverified findings, then everything the run could not check in full, then a line saying how much was checked, then one summary line, then the tokens the run consumed (see [Token Usage and Cost](#token-usage-and-cost)). This is a run with one violation and one ADR check that failed:

```
Violations:
src/auth.js
  [VIOLATION] 0007 No Secrets in Logs [Line 2]
    Reasoning: The login handler writes the user's password to the console.
    Code: console.log("password: " + user.password);

Failed ADR checks:
  src/flaky.js: ADR 0007 No Secrets in Logs: analysis failed after 3 retries: request timed out

Checked 2 file(s) against 3 ADR check(s); 4 file(s) had no relevant ADR.
1 new violation(s) in 1 file(s), 0 baselined. Not fully checked: 1 ADR check(s) failed.
Tokens: judgment 9840 input, 310 output; embeddings 620.
```

Each violation shows the ADR ID and title, the line, the reasoning, the quoted code, and the suggestion under `--suggest-fixes`. When the model wraps its quote in markdown (a fenced code block or inline backticks), the quote is matched, shown and baselined without the markdown, unless the backticks are part of the code itself. A run with nothing to report prints the coverage line, the result and the token line.

A finding is **unverified** when the code it quotes is not in what the model read: for a whole file, the file; for a large file analyzed through its diff, an added or unchanged line; under `--since`, an added or removed line. This happens when the model paraphrases, quotes the ADR instead of the code, or invents code. Unverified findings are listed under `Unverified findings` with their reasoning and quote, and counted in the summary, but they are not violations: a run whose only findings are unverified exits `0`. See [ADR 0031](docs/arch/0031-unverified-findings-and-the-exit-code.md).

```
Unverified findings (the quoted code was not found in what the model read, so they do not fail the run):
src/auth.js
  [UNVERIFIED] 0007 No Secrets in Logs
    Reasoning: The handler logs the session token.
    Code: logger.info(session.token)
```

Anything the run could not check in full is listed with its reason, and counted by kind in the summary:

- **Skipped files**: nothing in the file was checked. This includes a file too large to analyze in `--ci` mode.
- **Partly checked files**: the file was too large for the LLM, so only a truncated view of it was checked. In `--ci` mode such a file is skipped instead.
- **Failed ADR checks**: the file was checked against its other ADRs, but this ADR's verdict is missing, usually because the LLM request failed.
- **Failed stages**: a ranking stage with `on_error: fail` failed (see [Ranking Stages](#ranking-stages)).

The coverage line tells a clean run from one that checked nothing. A file is **judged** when at least one ADR check returned a verdict for it, and **ADR checks** counts those verdicts, cached ones included. A file has **no relevant ADR** when it was read in full and no ADR was left for it: none matched its `scope`, or every match scored below the threshold. When some of those files had candidates that all scored below the threshold, the line says how many, for example `5 file(s) had no relevant ADR, 4 of them because every candidate scored below the threshold.` That usually means the threshold is too high for your embedding model (see [Choosing `similarity_threshold`](#choosing-similarity_threshold)). Skipped and partly checked files are in the lists above and are not counted as judged; a partly checked file's verdicts still count as ADR checks, so a run can show more ADR checks than judged files. When no file was judged, a run with no violations says `No new architectural violations found, but no file was checked against an ADR.`, and adds that every candidate scored below the threshold when that is why. The exit code is the same either way, unless errors left the run with no ADR check, which exits `8`.

The log on stderr names each violation on one line, with or without `--debug`:

```
src/auth.js
  src/auth.js: violates ADR 0007 (No Secrets in Logs), line 2
```

With `--debug`, each ADR check also logs its outcome on one line: `compliant`, `violation`, `failed`, or `cached` added to either of the first two. Without `--debug`, a file with no finding that was not skipped logs nothing.

`archguard check --update-baseline` ends with a report in the same way: every recorded entry (file, ADR, line, reason), every violation it could not record and why (the quoted code is not in the file, so it could suppress nothing), and the files not fully checked, then the path written and the totals:

```
Recorded:
  src/auth.js: ADR 0007 No Secrets in Logs, line 2 (reason: accepted-debt)

Baseline written to archguard-baseline.json: 1 recorded, 0 not recorded.
```

### Token Usage and Cost

The report ends with the tokens the run consumed, as each provider reported them:

```
Tokens: judgment 48210 input, 1630 output (estimated cost 0.169080); embeddings 1240 (estimated cost 0.000025).
```

- **judgment** is the chat calls that judged a file against an ADR, retries included. A cached verdict makes no call and adds nothing.
- **suggestions** appears with `--suggest-fixes` and counts the remediation calls separately. A cached suggestion adds nothing.
- **embeddings** counts the embedding calls: one per file scored, plus any index rebuild the run triggered. `vector_store.provider` can differ from `llm.provider`, so it is reported on its own.

Calls that only count tokens to fit a large file into `llm.max_tokens` are not included. A figure is `unavailable`, never `0`, when the provider did not report it: Gemini's and Ollama's embedding endpoints never do, and an OpenAI-compatible server may leave it out. One unreported call makes its whole figure unavailable.

To turn tokens into a cost, set the price per 1,000 tokens in `archguard.yaml`: `llm.cost_per_1k_input_tokens` and `llm.cost_per_1k_output_tokens` (both or neither), and `vector_store.cost_per_1k_tokens`. ArchGuard has no built-in prices, and the cost is in whatever currency your prices are. **It is an estimate, not your bill:** it is the reported tokens times your prices, and it does not account for discounts, cached-input or batch pricing, failed calls the provider still billed, or price changes. See [ADR 0034](docs/arch/0034-token-usage-is-what-the-provider-reported.md).

`--dry-run` previews a run's cost without paying for the judgments. It embeds and scores files as usual, so its embedding figure is real, then counts the prompt each judgment would send with the chat provider's tokenizer instead of sending it. A judgment a cached verdict would answer costs nothing and is counted apart. The output tokens depend on the answer, so they are not projected and the estimated cost covers the input alone. The report lists what would be called instead of findings:

```
Dry run: 12 ADR check(s) would call the model and 3 would be answered from the cache; nothing was judged.
Tokens: judgment 48210 input, output not projected (estimated cost 0.144630); embeddings 1240 (estimated cost 0.000025).
```

With `--suggest-fixes` too, the cost of suggestions is not projected, since a dry run finds no violations to suggest a fix for, and the log says so in one line.

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
  "unverified": [],
  "coverage": { "files_judged": 2, "adr_checks": 3, "files_without_relevant_adr": 4, "files_below_threshold": 3 },
  "stages": [
    { "name": "rank", "received": 6, "kept": 4, "duration_ms": 812 },
    { "name": "rerank", "received": 4, "kept": 2, "duration_ms": 640 }
  ],
  "usage": {
    "judgment": { "input_tokens": 48210, "output_tokens": 1630, "estimated_cost": 0.16908, "calls": 10, "cache_hits": 3, "cache_misses": 9, "retries": 1 },
    "suggestion": { "input_tokens": 3120, "output_tokens": 210, "estimated_cost": 0.01251, "calls": 1, "cache_hits": 0, "cache_misses": 1, "retries": 0 },
    "embedding": { "tokens": null, "estimated_cost": null, "calls": 6 },
    "files": [
      { "file": "internal/api/handler.go", "duration_ms": 4210 },
      { "file": "internal/api/routes.go", "duration_ms": 1980 }
    ]
  }
}
```

`coverage` holds the same counts as the text report's coverage line. `files_judged: 0` means no file reached the model, which is not the same as a clean run; `files_without_relevant_adr` shows how many files had no ADR to be checked against, and `files_below_threshold` how many of those had candidate ADRs that all scored below the threshold.

`stages` lists every stage in the pipeline, in order, including the default `rank` stage when no `analysis.pipeline` is configured. For each stage, `received` and `kept` are the candidate ADRs it was handed and passed on, and `duration_ms` is the total time spent applying the stage (scoring, thresholding and, under `--debug`, writing its debug output), each summed across every file that reaches scoring (a file skipped earlier, such as a truncated file in `--ci` mode, adds nothing). Because files are checked concurrently, `duration_ms` can exceed the run's wall-clock time. A stage that received no candidates reports zeros. Use it to see how much each stage narrows the candidates and what that costs. `archguard check --debug` shows the same per file: the candidates each stage received, the ones it kept with their scores, and the ones it dropped with the reason.

When a stage with `on_error: fail` fails, the document also carries a `failures` array, each entry with the `stage`, the `file`, the `kind` (`unavailable` or `precondition_not_met`) and the underlying `error`; the array is omitted when nothing failed. The error text itself goes to stderr. The document has no field for skipped files or failed ADR checks, so a run that had any prints a one-line warning to stderr.

`usage` holds the same figures as the text report's token line (see [Token Usage and Cost](#token-usage-and-cost)). `suggestion` is present only with `--suggest-fixes`, and never under `--dry-run`. Under `--dry-run`, `judgment.output_tokens` is `null`, `judgment.estimated_cost` prices the input alone, and the document also carries `"dry_run": { "judgment_calls": 12, "cached": 3 }`. A count the provider did not report is `null`, and so is `estimated_cost` when no price is set or there is no count to price.

`usage` also says where the calls and time went, which the text report does not show:

- `calls` is every request sent to the provider, failed ones and retries included.
- `cache_hits` counts the ADR checks (or suggestions) answered from the cache, which make no call. `cache_misses` counts the ones that went to the model, once each however many retries they took, so `calls` is `cache_misses` plus `retries`. Under `--dry-run` a miss is a judgment the run would have sent, and `calls` is `0`.
- `embedding.calls` counts query embeddings and any index rebuild's, failed ones included.
- `files` lists every file the run checked, slowest first, with the time it took in `duration_ms`: reading it, scoring its candidates and every ADR check on it, waits between retries included. Files are checked concurrently, so the durations can add up to more than the run's wall-clock time.

`count` matches the number of new (non-baselined) violations that drives the `4` (drift detected) exit code above. `unverified` lists the unverified findings with the same fields, a `line` of `0`, and no `suggestion`; they are not counted in `count`. `suggestion` is present only when `--suggest-fixes` was passed; it's an LLM-generated pointer, not a verified or guaranteed fix, and it is omitted from the JSON entirely (not an empty string) when `--suggest-fixes` is off or the LLM produced nothing.

### Suppression

Intentionally ignore a violation for a specific file using a comment:

```go
// archguard-ignore: 0001
```

- The ignore token must match the **ADR ID** — by default the numeric prefix of the filename, or whatever `analysis.adr_id_pattern` extracts if configured.
- The comment must be in the file's first 2000 bytes. One further down is not seen.

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

This action automatically sets up Go, installs ArchGuard, and runs `archguard check --ci` on the files your pull request or push changed. If you set `provider: 'ollama'`, it will also automatically install and configure Ollama with the required models.

| Input | Default | Meaning |
|---|---|---|
| `provider` | required | The provider configured in `archguard.yaml`. |
| `scope` | `changed` | `changed` checks the files the event changed; `all` checks every tracked file. |
| `base` | the pull request base, or the commit before a push | The commit to compare against when `scope` is `changed`. |
| `debug` | `false` | `true` passes `--debug`, which logs each file and ADR the check judged. |
| `config` | `archguard.yaml` | A config file to read instead, relative to the repository root, for example a CI-only config. Passed as `--config`. |

The action fetches the base commit itself, so `actions/checkout` needs no `fetch-depth` change. When an event has no base (for example `workflow_dispatch` or the first push of a branch), the action checks every tracked file. Use it on `pull_request` events: under `pull_request_target` the default checkout is the base branch, so there is nothing to compare unless you check out the pull request's head yourself.

Inside GitHub Actions, each new (not baselined) violation is also printed as an error annotation, so it shows on the pull request next to the file and line that caused it, titled with the ADR's ID and title. Each unverified finding is printed as a warning annotation on its file. When the line isn't known, the annotation is on the file. GitHub shows at most 10 error annotations per step and 50 per job, so the report on stdout remains the full list. Annotations are not printed under `--format json`.

#### Other CI Providers

If you are not using GitHub Actions, run ArchGuard in your pipeline with the `--ci` flag. `--ci` needs a scope: use `--since <base commit>` to check what a change touched, or `--all` to check every tracked file.

**Warn-Open Policy:**
Large files may be truncated to fit the LLM context. In `--ci` mode, truncated files result in a **Warning** rather than a failure, ensuring your pipeline doesn't break due to inconclusive analysis on massive files.

---

## 🔬 Technical Details

- **Semantic Search**: Uses cosine similarity to find relevant ADRs based on the code being analyzed.
- **Index Optimizations**: Employs Delta Indexing to bypass redundant LLM API calls on unchanged files, concurrent provider routines to mask network latency, and conditional HNSW graph maintenance routines in Postgres.
- **Diff Analysis**: A file over the token limit that has uncommitted changes is analyzed through its diff. A violation's line is still its line in the file: the quoted code must be in an added or unchanged line of the diff, preferring an added one, and a quote that is only in a removed line is reported as not found.
- **Smart Truncation**: Files exceeding the token limit are rolled back to the nearest newline character to preserve code integrity during analysis.
- **Caching**: Analysis results are persisted in `.archguard/cache` based on a hash of the model, ADR content, and file content to reduce API costs and execution time.
- **Baseline Mode**: Run `archguard check --update-baseline` to snapshot every currently-detected violation into `archguard-baseline.json` at the repository root. Subsequent `archguard check` runs load it automatically (no extra flag needed) and treat a matching violation as already-known — excluded from the new-violation count and exit code, but still reported separately (e.g. "3 new violations, 12 baselined"). An entry is invalidated, and its violation re-surfaces as new, once the specific code it originally cited is no longer present in the file. Unlike `.archguard/index.json` and `.archguard/cache/`, `archguard-baseline.json` **is** committed to git, so a team's grandfathered violations travel with the repository. Suppression is per `(ADR, file)` pair, so a second, different violation of an already-baselined ADR in the same file stays hidden until the originally-cited code changes — periodically re-running `--update-baseline` is recommended to catch these.
- **Parallel Execution**: Coordinates analysis across files using a worker pool (defaulting to 5 concurrent workers).

## 🤝 Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for architectural overviews and technical standards.

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.
