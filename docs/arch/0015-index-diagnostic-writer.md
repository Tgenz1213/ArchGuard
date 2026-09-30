---
title: Diagnostic output for internal/index
status: Accepted
scope: "internal/**"
---

# Diagnostic output for internal/index

## Context

`docs/arch/0014-json-check-output.md` guarantees that `archguard check --format json` prints exactly one JSON document on stdout, with everything meant for a person on stderr. `internal/index` prints progress and warnings from several places that `check` reaches: `LocalStore.BuildIndex`/`PgStore.BuildIndex` (progress dots, per-ADR failures), `NewPgStore` (pgvector-version and `hnsw.iterative_scan` warnings), `CompositeProvider.GetADRs` (provider-fetch warnings), and `LocalProvider`/`ConfluenceProvider` (parse failures and malformed rules). `check` calls `GetADRs` on every run and `BuildIndex` whenever the local index hash no longer matches the ADR corpus, so any of these printing straight to stdout corrupts the JSON document. See #70, #162, #163.

## Decision

Every diagnostic in `internal/index` goes through an `*output.Printer` supplied by the caller:

- `LocalStore` and `PgStore` hold an unexported `out *output.Printer`. `NewPgStore` takes it as its last parameter because it prints while it is being constructed; `NewLocalStore` has no construction-time output, so `NewVectorStore(cfg, out)` sets the field after construction.
- `LocalProvider`, `ConfluenceProvider` and `CompositeProvider` take theirs through `SetPrinter`, mirroring `SetIDPattern`, since none of them print during construction.
- A nil `*output.Printer` is valid and writes to stderr. A store or provider nobody wired up can therefore never print into a JSON report on stdout, and the many tests that construct stores with no printer need no change.
- `cli.runIndex` takes an `*output.Printer`: one over `os.Stdout` for `archguard index`, or `runCheck`'s own printer (over `human`) for `check`'s rebuild. `runCheck` passes that printer to `NewVectorStore` and a buffered `Group` of it to its providers, flushed unless a rebuild is about to repeat the same warnings.

## Consequences

- A new diagnostic in `internal/index` must use the store's or provider's `Printer`, never a bare `fmt.Print*` or `os.Stdout`.
- `output.Printer` serializes its own writes, so `BuildIndex`'s embed worker pool prints progress and failures from many goroutines without holding its own lock around them, even with a caller-supplied writer such as a `bytes.Buffer`.
- A test that wants to read the output of a store built with no printer captures `os.Stderr`, as `pgvector_integration_test.go`'s `captureStderr` does; the `Printer` reads `os.Stderr` at each write, so redirecting it after construction works.
