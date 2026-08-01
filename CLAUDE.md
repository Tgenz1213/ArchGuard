# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ArchGuard is a Go CLI (and GitHub Action) that detects "architectural drift": it uses an LLM to check whether staged/changed code violates rules written in Architectural Decision Records (ADRs — markdown files with YAML frontmatter). It embeds ADRs into a vector store, finds ADRs semantically relevant to a changed file, then asks an LLM provider (Ollama, OpenAI, or Gemini) a literal yes/no compliance question per relevant ADR. Notably, this repo dogfoods its own tooling: its architectural decisions live in `docs/arch/` and are themselves checked by ArchGuard.

## Commands

- Build: `go build -o archguard ./cmd/archguard`
- Install: `go install ./cmd/archguard`
- Run all tests: `go test ./...`
- Run with race + coverage (matches CI): `go test -v -race -cover ./...`
- Run a single test: `go test ./internal/analysis -run TestName -v`
- Lint (matches CI): `golangci-lint run --timeout=5m`
- Local release dry-run: `goreleaser release --snapshot --clean`
- Exercise the CLI locally: `archguard init` → `archguard index` → `archguard check --staged` (or `--all`, `--ci`, `--debug`, or a specific path)

CI (`.github/workflows/ci.yml`) runs `golangci-lint`, then `go test -v -race -cover ./...` and a build, on Go 1.26. Postgres/pgvector integration tests (`internal/index/pgvector_integration_test.go`) use `testcontainers-go` and require Docker; the rest of the suite doesn't.

## Architecture

Execution flow, in order: `cmd/archguard/main.go` → `internal/cli.Execute` → `internal/analysis.Engine.Run`.

- **`internal/cli`**: Parses args, finds the git root and `chdir`s into it (all paths are resolved relative to repo root, not cwd), loads `archguard.yaml` via `internal/config`, constructs the configured `llm.Provider`, and dispatches to `init` / `index` / `check`. `Execute` takes a `providerFactory` injection point used by tests to swap in a mock provider — this is how `test/e2e_test.go` avoids hitting real LLM APIs or Ollama.
- **`internal/config`**: Defines `Config` (YAML-backed). `ARCHGUARD_DB_URL` env var overrides `vector_store.connection_string`; `ARCHGUARD_API_KEY` env var supplies the OpenAI/Gemini API key (read in `cli`, not `config`).
- **`internal/index`**: ADR ingestion and the vector store.
  - `Provider` interface fetches ADRs from a source; `LocalProvider` reads `analysis.adr_path` from disk, `ConfluenceProvider` pulls from Atlassian Confluence (REST API v2). `CompositeProvider` fans out to all configured providers concurrently and merges results — a single provider failing doesn't fail the run unless *all* fail.
  - `VectorStore` interface (`LocalStore` = JSON file at `.archguard/index.json`; `PgStore` = Postgres + pgvector with HNSW) is chosen by `NewVectorStore` based on whether `vector_store.connection_string` is set.
  - **Delta Indexing**: `BuildIndex` skips re-embedding an ADR if its `RelPath`, `Content`, `Title`, and `Status` are unchanged from the existing index — only new/modified ADRs hit the embedding API.
  - The index is hash-validated (`CalculateHash` over model name + ADR content) against the saved index on every `check`; a mismatch triggers an automatic `runIndex` rebuild before proceeding.
- **`internal/analysis`**: The core engine (`engine.go`).
  - `ContentProvider` abstracts *which* files to scan and how to read them: `UncommittedProvider` (default), `StagedProvider` (`--staged`), `AllProvider` (`--all` or a `.` path arg), `SingleFileProvider` (explicit path arg).
  - `Engine.Run` fans out over files with a bounded worker pool (`analysis.max_concurrency`, default 5) via `errgroup`. Per file: fetch context → embed it → search the vector store for relevant ADRs (cosine similarity, `similarity_threshold`, top-3) → filter by ADR `scope` glob and `archguard-ignore: <ADR_ID>` suppression comments → call the LLM for each remaining ADR.
  - **Context sizing** (`fetchContext`): counts the file against `llm.max_tokens` using a tiktoken encoder keyed off `LLM.Model` (falling back to `cl100k_base` for unrecognized model names, e.g. Ollama models), and if it fits, uses the content whole; otherwise prefers a diff (if available) over truncation. **Smart Truncation** rolls truncated content back to the nearest preceding newline so files aren't cut mid-line.
  - **Caching**: LLM analysis results are cached in `.archguard/cache/<sha256>.json`, keyed by model name + ADR content + file content + system prompt + prompt template (`internal/cache`). A cache hit skips the LLM call entirely.
  - **CI Warn-Open policy**: in `--ci` mode, a file that had to be truncated produces a warning instead of running analysis on a possibly-incomplete context — avoids failing a pipeline on inconclusive input.
- **`internal/llm`**: `Provider` interface (`CreateEmbedding`, `Chat`) with `OpenAIProvider`, `OllamaProvider`, `GeminiProvider` implementations, plus a `MockProvider` (`mock.go`) for tests. This is the extension point for a new provider (e.g. Anthropic/Claude) — implement the interface here.
- **`internal/git`**: All git plumbing (repo root discovery, staged/uncommitted/tracked file lists, diffs) shells out to `git`.
- ADRs are matched to files structurally via YAML frontmatter `scope` (a glob, matched with `internal/analysis/glob.go`'s `matchGlob`, supporting `**`) and semantically via embedding similarity — both must pass for an ADR to apply to a given file.

## Conventions

- Standard Go layout: `cmd/` for thin entrypoints, `internal/` for everything else.
- ADR files require YAML frontmatter with `title` and `status`; `scope` is optional. `status` must appear in `analysis.accepted_statuses` (or use `["*"]`) to be considered.
- Architectural Decision Records for this codebase's own design live in `docs/arch/` — check there before making a design decision that might already be settled.
- Tests are table-driven where the cases share shape, and provider tests (`internal/llm/*_test.go`) mock the backend with `net/http/httptest` rather than hitting a live API — no live network calls in unit tests. `test/e2e_test.go` drives the full CLI through `cli.Execute` with an injected `MockProvider`.
- Conventional Commits for commit messages (`feat: ...`, `fix: ...`, `docs: ...`, `refactor: ...`, `build(deps): ...`).
- Exit codes are meaningful and tested: `0` success, `1` general error, `2` usage, `3` config, `4` drift detected, `5` index error — preserve these in `internal/cli` if you touch command dispatch.
