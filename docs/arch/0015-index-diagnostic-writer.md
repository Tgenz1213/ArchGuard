---
title: Diagnostic writer for internal/index
status: Accepted
scope: "internal/**"
---

# Diagnostic writer for internal/index

## Context

`docs/arch/0014-json-check-output.md` gave `archguard check --format json` a stdout-purity guarantee: stdout carries exactly one JSON document, with the startup banner, debug logging, and per-file progress redirected to stderr via `cli.runCheck`'s `human` writer. That guarantee held for everything routed through `cli.runCheck`/`analysis.Engine`, but `internal/index` had roughly 20 direct `fmt.Print*` call sites writing straight to `os.Stdout` regardless of caller: `LocalStore.BuildIndex`/`PgStore.BuildIndex`'s progress and per-ADR warnings, `NewPgStore`'s pgvector-version warnings, `CompositeProvider.GetADRs`'s provider-fetch warnings, and `LocalProvider`/`ConfluenceProvider`'s per-file parse-failure warnings. `runCheck` calls into several of these paths on every `check` run -- `adrProvider.GetADRs` always, and `runIndex`'s `BuildIndex` whenever the on-disk index's hash doesn't match the current ADR corpus (the normal path on first run or after any ADR edit) -- so a `--format json` run hitting one of them got non-JSON text ahead of or interleaved with the JSON document on stdout, corrupting it for any consumer parsing stdout as JSON. See #70, #162, #163.

## Decision

`internal/index`'s stores and providers each gained a configurable diagnostic writer instead of hardcoding `os.Stdout`:

- `LocalStore` and `PgStore` gained an unexported `writer io.Writer` field. `PgStore`'s constructor, `NewPgStore`, takes it as a new trailing parameter (`NewPgStore(connStr, projectName string, concurrency int, hnsw HNSWOptions, w io.Writer)`) because it prints diagnostics -- pgvector-version and `hnsw.iterative_scan` warnings -- *during* construction, before a struct exists to attach a field to afterward. `LocalStore` has no construction-time diagnostics, so `NewLocalStore`'s signature was left unchanged; its `writer` field is set directly by `NewVectorStore` after construction.
- `LocalProvider`, `ConfluenceProvider`, and `CompositeProvider` each gained the same `writer io.Writer` field plus a `SetWriter(io.Writer)` method, mirroring the existing `(*LocalProvider).SetIDPattern` pattern -- a setter rather than a constructor parameter, since none of their diagnostics happen during construction and a setter avoids changing three widely-used constructor signatures across ~15 existing test call sites.
- A package-level `diagWriter(w io.Writer) io.Writer` helper resolves a `nil` writer to `os.Stdout`, evaluated **dynamically at each write call**, never cached at construction time. This matters because several existing tests (`internal/index/pgvector_integration_test.go`'s `captureStdout`) redirect the *global* `os.Stdout` variable around a call to a store/provider that was constructed earlier with a `nil` writer; caching the resolved writer at construction would have captured the pre-redirect `os.Stdout` value and broken those tests' output capture.
- `internal/index.NewVectorStore` gained a second parameter, `w io.Writer`, forwarded to whichever backend it constructs.
- `internal/cli.runIndex` gained a trailing `w io.Writer` parameter. `cli.Execute`'s call for the standalone `archguard index` command always passes `os.Stdout` (that command has no `--format` flag and its output is unaffected by JSON mode); `cli.runCheck`'s index-rebuild call site passes its existing `human` writer (stderr in `--format json` mode, stdout otherwise), the same writer it already passed to `analysis.Engine.Writer` per #0014. `runCheck` also calls `SetWriter(human)` on every `index.Provider` it constructs and passes `human` into `index.NewVectorStore`.

## Consequences

- Any future diagnostic print added to `internal/index` must go through `diagWriter(s.writer)` / `diagWriter(p.writer)`, not a bare `fmt.Print*`, or it reintroduces the stdout leak this ADR closes.
- `BuildIndex`'s embed worker pool writes progress/failure text to the configured writer from multiple goroutines. Production always passes `*os.File` (safe for concurrent writes), but an arbitrary caller-supplied `io.Writer` is not guaranteed to be -- `BuildIndex` serializes these writes under its existing failure-tracking mutex rather than assuming writer safety.
- `NewPgStore`'s signature changed (a new required trailing parameter); every direct caller -- including `internal/index/pgvector_bench_test.go` and `internal/index/pgvector_integration_test.go`'s ~29 call sites -- was updated to pass `nil` (meaning "use the live stdout, resolved dynamically"), preserving prior behavior exactly.
- `internal/index.NewVectorStore`'s signature changed to take a writer; its only callers, both in `internal/cli.go`, were updated. No other package calls it.
- This closes the "Known gap" `docs/arch/0014-json-check-output.md` documented and tracked as #163: `archguard check --format json` now stays valid JSON on stdout even when it triggers an index rebuild or hits a provider-fetch warning.
