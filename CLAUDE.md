# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

ArchGuard is a Go CLI that detects "architectural drift": it uses an LLM to check whether staged/changed code violates rules written in Architectural Decision Records (ADRs — markdown files with YAML frontmatter). It embeds ADRs into a vector store, finds ADRs semantically relevant to a changed file, then asks an LLM provider (Ollama, OpenAI, or Gemini) a literal yes/no compliance question per relevant ADR. Notably, this repo dogfoods its own tooling: its architectural decisions live in `docs/arch/` and are themselves checked by ArchGuard.

It ships two ways: as a CLI binary a developer runs locally (e.g. as a pre-commit check), and as a first-class GitHub Action (`action.yml` at repo root, published to the GitHub Marketplace) that runs `archguard check --ci` in a consumer's CI pipeline. These aren't two separate implementations — the Action builds and runs the same `cmd/archguard` binary from source, just wired for CI. Don't assume "CLI usage" is the only consumer of `internal/cli` behavior; changes to `check`'s flags, exit codes, or `--ci` behavior affect the Action too.

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

- **`internal/cli`**: Parses args, finds the git root and `chdir`s into it (all paths are resolved relative to repo root, not cwd), loads `archguard.yaml` via `internal/config`, constructs the configured `llm.Provider`, and dispatches to `init` / `index` / `check`. `Execute` takes a `providerFactory` injection point for swapping in a mock provider — `cmd/archguard-e2e/main.go` is a second binary entrypoint that calls `Execute` with exactly that, a mock factory, so `test/e2e_test.go` can build and exec that binary as a subprocess instead of hitting real LLM APIs or Ollama.
- **`internal/config`**: Defines `Config` (YAML-backed). `ARCHGUARD_DB_URL` env var overrides `vector_store.connection_string`; `ARCHGUARD_API_KEY` env var supplies the OpenAI/Gemini API key (read in `cli`, not `config`).
- **`internal/index`**: ADR ingestion and the vector store.
  - `Provider` interface fetches ADRs from a source; `LocalProvider` reads `analysis.adr_path` from disk, `ConfluenceProvider` pulls from Atlassian Confluence (REST API v2). `CompositeProvider` fans out to all configured providers concurrently and merges results — a single provider failing doesn't fail the run unless *all* fail.
  - `VectorStore` interface (`LocalStore` = JSON file at `.archguard/index.json`; `PgStore` = Postgres + pgvector with HNSW) is chosen by `NewVectorStore` based on whether `vector_store.connection_string` is set.
  - **Delta Indexing**: `BuildIndex` skips re-embedding an ADR if its `RelPath`, `Content`, `Title`, and `Status` are unchanged from the existing index — only new/modified ADRs hit the embedding API.
  - **This differs by backend.** `LocalStore`: on every `check`, `CalculateHash` (vector-store model name + each ADR's `RelPath` and `Content`) is compared against the hash saved in `.archguard/index.json`; a mismatch triggers an automatic `runIndex` rebuild before proceeding. `PgStore`: `CalculateHash` is a constant (`"remote"`) and `Load` does no hash comparison at all — it only ensures the `archguard_adrs` table and HNSW index exist, so Postgres-backed setups never auto-trigger a rebuild from a hash mismatch the way local ones do.
- **`internal/analysis`**: The core engine (`engine.go`).
  - `ContentProvider` abstracts *which* files to scan and how to read them: `UncommittedProvider` (default), `StagedProvider` (`--staged`), `AllProvider` (`--all` or a `.` path arg), `SingleFileProvider` (explicit path arg).
  - `Engine.Run` fans out over files with a bounded worker pool (`analysis.max_concurrency`, default 5) via `errgroup`. Per file: fetch context → strip unified-diff patch metadata (`stripDiffMetadata`; a no-op on non-diff input, e.g. the whole-file-content fallback) → embed it → search the vector store for relevant ADRs (cosine similarity, `similarity_threshold`, top-3) → filter by ADR `scope` glob and `archguard-ignore: <ADR_ID>` suppression comments → call the LLM for each remaining ADR.
  - **Context sizing** (`fetchContext`): counts the file against `llm.max_tokens` via `Provider.CountTokens` — each provider counts using its own real tokenizer/backend rather than one hardcoded encoder (see `docs/arch/0002-provider-scoped-token-counting.md`). If it fits, uses the content whole; otherwise prefers a diff (if available) over truncation (`truncateToTokenLimit`), which estimates a byte cutoff and verifies/shrinks it via further `CountTokens` calls, guaranteeing the result stays within budget. **Smart Truncation** rolls truncated content back to the nearest preceding newline so files aren't cut mid-line.
  - **Caching**: LLM analysis results are cached in `.archguard/cache/<sha256>.json`, keyed by model name + ADR content + file content + system prompt + prompt template (`internal/cache`). A cache hit skips the LLM call entirely.
  - **CI Warn-Open policy**: in `--ci` mode, a file that had to be truncated produces a warning instead of running analysis on a possibly-incomplete context — avoids failing a pipeline on inconclusive input.
- **`internal/llm`**: `Provider` interface (`CreateEmbedding`, `Chat`, `CountTokens`) with `OpenAIProvider`, `OllamaProvider`, `GeminiProvider` implementations, plus a `MockProvider` (`mock.go`) for tests. This is the extension point for a new provider (e.g. Anthropic/Claude) — implement the interface here. `CreateEmbedding` takes an `EmbeddingTaskType` (`EmbeddingTaskDocument` for indexing, `EmbeddingTaskQuery` for search) so a provider with an asymmetric-retrieval mechanism can use it: `GeminiProvider` maps it to the Gemini API's own `TaskType` string, `OllamaProvider` maps it to nomic-embed-text's `search_document:`/`search_query:` prefix convention (only when the configured embed model name starts with `nomic-embed`), and `OpenAIProvider` ignores it (see `docs/arch/0003-embedding-task-role.md`).
- **`internal/git`**: All git plumbing (repo root discovery, staged/uncommitted/tracked file lists, diffs) shells out to `git`.
- ADRs are matched to files structurally via YAML frontmatter `scope` (a glob, matched with `internal/analysis/glob.go`'s `matchGlob`, supporting `**`) and semantically via embedding similarity — both must pass for an ADR to apply to a given file.
- **`action.yml`**: A composite GitHub Action, not a separate service — it `go install`s `./cmd/archguard` from the action's own checked-out source at run time, optionally installs Ollama and pulls `llama3.2` + `nomic-embed-text` (only when `inputs.provider == 'ollama'`), then runs `archguard check --ci`. Its only input is `provider`; everything else (model, thresholds, ADR path, exclude patterns) comes from the consumer's own `archguard.yaml`, same as local CLI usage. This is the CI Warn-Open path referenced above, not a distinct code path in `internal/analysis`.

## Conventions

- Standard Go layout: `cmd/` for thin entrypoints, `internal/` for everything else.
- ADR files require YAML frontmatter with `title` and `status`; `scope` is optional. `status` must appear in `analysis.accepted_statuses` (or use `["*"]`) to be considered.
- Architectural Decision Records for this codebase's own design live in `docs/arch/` — check there before making a design decision that might already be settled.
- Tests are table-driven where the cases share shape, and provider tests (`internal/llm/*_test.go`) mock the backend with `net/http/httptest` rather than hitting a live API — no live network calls in unit tests. `test/e2e_test.go` builds and execs the `cmd/archguard-e2e` binary (which wires `cli.Execute` to a `MockProvider` factory) rather than calling `cli.Execute` in-process.
- Conventional Commits for commit messages (`feat: ...`, `fix: ...`, `docs: ...`, `refactor: ...`, `build(deps): ...`).
- Exit codes are meaningful and tested: `0` success, `1` general error, `2` usage, `3` config, `4` drift detected, `5` index error — preserve these in `internal/cli` if you touch command dispatch.

## Known footguns / non-obvious behavior

- **`cli.Execute` rewrites `os.Args` in place before any flag parsing.** Every argument from index 2 onward that doesn't start with `-` is treated as a path and rewritten relative to the repo root, then the process `chdir`s there. This runs regardless of which subcommand is selected. If you add a subcommand with a non-path positional argument, it will still get run through `filepath.Rel` against the repo root — verify it survives that rewrite, especially in tests that invoke `Execute` from a non-root working directory.
- **`OllamaProvider.CountTokens` must use `num_predict: 1`, not `0`.** `0` does not suppress generation on this codebase's Ollama version (it still runs a full completion); `1` reliably stops after exactly one token and still returns the real `prompt_eval_count`. See `docs/arch/0002-provider-scoped-token-counting.md` for the full rationale. `CountTokens` for Ollama/Gemini is also a live network call, so `truncateToTokenLimit` can cost several round-trips per oversized file — don't assume truncation is free local computation when touching that path.
- **`CompositeProvider.GetADRs` swallows individual provider failures.** If one ADR source (e.g. Confluence) errors, it prints a `Warning: failed to fetch ADRs from a provider` and continues with whatever the other providers returned; the run only fails if *every* provider fails. A partial/reduced ADR set from a flaky Confluence connection will not surface as an error — check console output, don't assume a clean `check` run means all configured ADR sources were reachable.
- **`OllamaProvider.CreateEmbedding` silently skips the nomic prefix for any embed model not named `nomic-embed*`.** There's no error or warning — a typo'd or swapped `vector_store.embedding_model` just stops getting the `search_document:`/`search_query:` prefix with no signal that asymmetric retrieval quietly turned off.
- **`archguard.yaml` is not gitignored, but `analysis.confluence.token` lives in it in plaintext** (unlike the DB connection string and LLM API key, which both have environment-variable overrides — `ARCHGUARD_DB_URL`, `ARCHGUARD_API_KEY`). Don't commit a real Confluence token into `archguard.yaml`; if you're adding config secrets, prefer wiring them through an env var like the existing two rather than a YAML field.

## Keeping this file and the ADRs current

- Update this file when you change build/test/lint commands, add or rename a top-level package, change a cross-package interface (e.g. `llm.Provider`, `index.VectorStore`, `analysis.ContentProvider`), or discover a footgun while debugging — add it to the list above rather than letting it get rediscovered.
- When a change embodies an architecturally-significant decision — a new invariant, a rejected alternative worth remembering, a cross-cutting constraint on future providers/stores — write an ADR in `docs/arch/` (see `docs/ADR_TEMPLATE.md`) instead of only describing it here. This repo dogfoods ArchGuard's own drift checking against `docs/arch/`, so an undecided or undocumented convention can't be enforced by the tool itself. Keep this file's job to pointing at *where* the decision lives and summarizing *what* it constrains, not re-deriving the reasoning an ADR already owns.
