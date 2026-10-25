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

The module builds on Go 1.27, so a golangci-lint built with an older Go can't lint it. In cloud sessions, `.claude/hooks/session-start.sh` installs one built with `go.mod`'s Go version and puts it first on `PATH`.

Git hooks in `.githooks/` block a commit on `main`, a commit while lint reports issues in changed Go code, and a push of a branch that doesn't contain `origin/main` unless it is stacked on another open branch. The SessionStart hook turns them on; outside Claude, run `git config core.hooksPath .githooks` once per clone. `--no-verify` is denied in `.claude/settings.json`.

Claude may not read `.env`: deny rules in `.claude/settings.json` and the `block-env.sh` Bash hook refuse it (`docs/arch/0042-claude-may-not-read-env.md`). A recursive search that never names the file still gets through, so keep API keys in environment variables rather than `.env`.

## Architecture

Execution flow, in order: `cmd/archguard/main.go` → `internal/cli.Execute` → `internal/analysis.Engine.Run`.

| Area | Paths | Guidance |
|---|---|---|
| CLI: command line, startup, providers, `check` output, exit codes | `internal/cli/`, `cmd/archguard/` | `.claude/rules/cli.md` |
| Config: `archguard.yaml`, pipeline stages, env overrides | `internal/config/`, `archguard.yaml` | `.claude/rules/config.md` |
| Index: ADR providers, vector stores, Postgres | `internal/index/`, `docker-compose.yml` | `.claude/rules/index.md` |
| Analysis: the engine, scoring stages, context sizing, cache | `internal/analysis/`, `internal/cache/` | `.claude/rules/analysis.md` |
| Inference: chat and embedding providers | `internal/inference/` | `.claude/rules/inference.md` |
| Output: the `Printer`, reports, color | `internal/output/` | none; see its `doc.go` |
| Baseline: `archguard-baseline.json` | `internal/baseline/` | none; see its `doc.go` |
| Git plumbing | `internal/git/` | none; see its `doc.go` |
| CI workflows and the GitHub Action | `.github/`, `action.yml` | `.claude/rules/ci.md` |
| ADR docs | `docs/arch/` | `.claude/rules/adr-docs.md` |
| Tests | `*_test.go`, `test/`, `internal/testutil/`, `cmd/archguard-e2e/` | `.claude/rules/tests.md` |

Each package's `doc.go` says what the package is for, what it does not own and its key terms; `go doc ./internal/<pkg>` prints it.

`internal/atomicfile` has no rules file: its `Write` is the one write-tmp-then-rename helper, used by the baseline, the local index and `check --output`. `internal/telemetry` has none either: its `Recorder` is the one seam through which a run reports what it did (`docs/arch/0035-run-recorder-seam.md`).

## Conventions

- Standard Go layout: `cmd/` for thin entrypoints, `internal/` for everything else.
- Architectural Decision Records for this codebase's own design live in `docs/arch/` — check there before making a design decision that might already be settled.
- Conventional Commits for commit messages (`feat: ...`, `fix: ...`, `docs: ...`, `refactor: ...`, `build(deps): ...`).
- **Dispatch subagents as `writer` (edits or commits) or `reviewer` (reads only)** from `.claude/agents/`. A general-purpose subagent shares the session's checkout and can commit on the wrong branch or reset the work it reviews (`docs/arch/0040-isolated-subagents.md`).
- **`.worktreeinclude` copies `.archguard/` and `.serena/project.local.yml` into each new worktree,** so a writer or `--worktree` session skips the index rebuild. It never lists `.env`.
- **Coding standards are in `docs/standards/`** (start at its `README.md`). Read the page for what you're writing, and see `docs/arch/0029-coding-standards.md` for why they're binding.

## Ticket & PR Conventions

- Issues use the templates in `.github/ISSUE_TEMPLATE/` (bug report / feature request); PRs use `.github/PULL_REQUEST_TEMPLATE.md`.
- Every issue needs testable **acceptance criteria** — concrete pass/fail conditions (exit codes, specific behavior, "test X added"), not a vague problem description — before work starts on it. If an issue lacks them, add them first rather than implementing against an ambiguous ask.
- PR and commit titles follow Conventional Commits (see above). Link issues with `Closes #N`.
- An architecturally-significant PR should add an ADR under `docs/arch/` (see "Keeping this guidance and the ADRs current" below) — self-apply this when opening PRs autonomously.

## Writing memories

- A memory's **Why** says what goes wrong without the rule, never who asked for it or the story of a correction; another session needs the reason to judge a new case.
- One rule per memory, in a few lines.
- Never save what a repo file, hook, permission or lint rule already holds; it drifts from the original. Delete a memory once a tool enforces it.
- No counts or issue lists that go stale; link other memories by their exact `name:` slug.

## Keeping this guidance and the ADRs current

- Where each kind of fact goes, and what guidance may not contain, is in `docs/standards/guidance.md`.
- Each rules file's `paths` frontmatter lists the files that load it; a new package or area gets a row in the Architecture table and either its own rules file or a path added to an existing one.
- Update this file or the matching rules file when you change build/test/lint commands, add or rename a top-level package, change a cross-package interface (e.g. `inference.Chatter`, `inference.Embedder`, `index.VectorStore`, `analysis.ContentProvider`), or discover a footgun while debugging.
- A real decision gets an ADR in `docs/arch/` (see `docs/ADR_TEMPLATE.md`). This repo checks its own changes against `docs/arch/`, so a convention with no ADR can't be enforced by the tool.
