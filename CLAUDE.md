# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository. It holds what applies to every task; guidance for one area is in `.claude/rules/` and loads when a matching file is read or edited.

## What this is

ArchGuard is a Go CLI that detects "architectural drift": it uses an LLM to check whether staged/changed code violates rules written in Architectural Decision Records (ADRs — markdown files with YAML frontmatter). It embeds ADRs into a vector store, finds ADRs semantically relevant to a changed file, then asks an LLM provider (Ollama, OpenAI, Gemini, or Claude) a literal yes/no compliance question per relevant ADR. Notably, this repo dogfoods its own tooling: its architectural decisions live in `docs/arch/` and are themselves checked by ArchGuard.

It ships two ways: as a CLI binary a developer runs locally (e.g. as a pre-commit check), and as a first-class GitHub Action (`action.yml` at repo root, published to the GitHub Marketplace) that runs `archguard check --ci` in a consumer's CI pipeline. These aren't two separate implementations — the Action builds and runs the same `cmd/archguard` binary from source, just wired for CI. Don't assume "CLI usage" is the only consumer of `internal/cli` behavior; changes to `check`'s flags, exit codes, or `--ci` behavior affect the Action too.

## Commands

- Build: `go build -o archguard ./cmd/archguard`
- Install: `go install ./cmd/archguard`
- Run the unit tests: `go test ./...`
- Run the Postgres integration tests (requires Docker): `go test -tags integration ./internal/index`
- Run the e2e tests: `go test -tags e2e ./test/e2e`
- Run unit tests with race + coverage (matches CI): `go test -v -race -cover ./...`
- Run a single test: `go test ./internal/analysis -run TestName -v`
- Lint (matches CI): `golangci-lint run --timeout=5m`
- Local release dry-run: `goreleaser release --snapshot --clean`
- Exercise the CLI locally: `archguard init` → `archguard index` → `archguard check --staged` (or `--all`, `--ci --all`, `--debug`, or a specific path)

The module builds on Go 1.27, so a golangci-lint built with an older Go can't lint it.

## Architecture

Execution flow, in order: `cmd/archguard/main.go` → `internal/cli.Execute` → `internal/analysis.Engine.Run`.

| Area | Paths | Guidance |
|---|---|---|
| CLI: command line, startup, providers, `check` output, exit codes | `internal/cli/`, `cmd/archguard/` | `.claude/rules/cli.md` |
| Config: `archguard.yaml`, pipeline stages, env overrides | `internal/config/`, `archguard.yaml` | `.claude/rules/config.md` |
| Index: ADR providers, vector stores, Postgres | `internal/index/`, `docker-compose.yml` | `.claude/rules/index.md` |
| Analysis: the engine, scoring stages, context sizing, cache | `internal/analysis/`, `internal/cache/` | `.claude/rules/analysis.md` |
| Inference: chat and embedding providers | `internal/inference/` | `.claude/rules/inference.md` |
| Output: the `Printer`, reports, color | `internal/output/` | `.claude/rules/output.md` |
| Baseline: `archguard-baseline.json` | `internal/baseline/` | `.claude/rules/baseline.md` |
| Git plumbing | `internal/git/` | `.claude/rules/git.md` |
| CI workflows and the GitHub Action | `.github/`, `action.yml` | `.claude/rules/ci.md` |
| ADR docs | `docs/arch/` | `.claude/rules/adr-docs.md` |
| Tests | `*_test.go`, `test/`, `internal/testutil/`, `cmd/archguard-e2e/` | `.claude/rules/tests.md` |

`internal/atomicfile` has no rules file: `Write(path string, data []byte) error` is the shared write-tmp-then-rename helper used by `baseline.Save` and `index.LocalStore.Save`, so both get identical atomic-write-and-cleanup-on-failure behavior instead of two independently-maintained (and already-diverged) copies.

## Conventions

- Standard Go layout: `cmd/` for thin entrypoints, `internal/` for everything else.
- Architectural Decision Records for this codebase's own design live in `docs/arch/` — check there before making a design decision that might already be settled.
- Conventional Commits for commit messages (`feat: ...`, `fix: ...`, `docs: ...`, `refactor: ...`, `build(deps): ...`).
- **Coding standards are in `docs/standards/`** (start at its `README.md`). Read the page for what you're writing, and see `docs/arch/0029-coding-standards.md` for why they're binding.

## Ticket & PR Conventions

- Issues use the templates in `.github/ISSUE_TEMPLATE/` (bug report / feature request); PRs use `.github/PULL_REQUEST_TEMPLATE.md`.
- Every issue needs testable **acceptance criteria** — concrete pass/fail conditions (exit codes, specific behavior, "test X added"), not a vague problem description — before work starts on it. If an issue lacks them, add them first rather than implementing against an ambiguous ask.
- PR and commit titles follow Conventional Commits (see above). Link issues with `Closes #N`.
- An architecturally-significant PR should add an ADR under `docs/arch/` (see "Keeping this guidance and the ADRs current" below) — self-apply this when opening PRs autonomously.

## Keeping this guidance and the ADRs current

- Guidance lives in this file and in the rules files under `.claude/rules/` named in the Architecture table. A fact goes in exactly one place: here if it applies to every task, otherwise in the rules file for the area it concerns. Each rules file's `paths` frontmatter lists the files that load it; a new package or area gets a row in the table and either its own rules file or a path added to an existing one.
- Update this file or the matching rules file when you change build/test/lint commands, add or rename a top-level package, change a cross-package interface (e.g. `inference.Chatter`, `inference.Embedder`, `index.VectorStore`, `analysis.ContentProvider`), or discover a footgun while debugging — add it to that area's footguns rather than letting it get rediscovered.
- When a change embodies an architecturally-significant decision — a new invariant, a rejected alternative worth remembering, a cross-cutting constraint on future providers/stores — write an ADR in `docs/arch/` (see `docs/ADR_TEMPLATE.md`) instead of only describing it in guidance. This repo dogfoods ArchGuard's own drift checking against `docs/arch/`, so an undecided or undocumented convention can't be enforced by the tool itself. Keep the guidance's job to pointing at *where* the decision lives and summarizing *what* it constrains, not re-deriving the reasoning an ADR already owns.
