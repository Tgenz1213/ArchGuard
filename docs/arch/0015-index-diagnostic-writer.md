---
title: Diagnostic output for internal/index
status: Accepted
scope: "internal/**"
---

# Diagnostic output for internal/index

## Context

`docs/arch/0014-json-check-output.md` guarantees that `archguard check --format json` prints exactly one JSON document on stdout, with everything meant for a person on stderr. `internal/index` prints progress and warnings from places `check` reaches: building the index, setting up the Postgres store, and fetching and parsing ADRs. `check` fetches ADRs on every run and rebuilds the index whenever its hash stops matching the corpus, so any of these printing straight to stdout corrupts the JSON document. See #70, #162, #163.

## Decision

Every diagnostic in `internal/index` goes through an `*output.Printer` supplied by the caller:

- Each vector store and ADR provider holds its own printer. One that prints while it is being constructed takes the printer as a constructor parameter; the others take it through a setter.
- A nil `*output.Printer` is valid and writes to stderr. A store or provider nobody wired up can therefore never print into a JSON report on stdout, and tests that construct stores with no printer need no change.
- `archguard index` prints its diagnostics on stdout. `check`'s rebuild prints through a quiet copy of `check`'s log printer, which shows info lines only under `--debug` and sums the rebuild up in one line. `check` buffers its providers' warnings and flushes them, unless a rebuild is about to repeat the same warnings.

## Rules

- Every diagnostic in `internal/index` MUST go through an `*output.Printer` supplied by the caller.
- Each vector store and ADR provider MUST hold its own printer, taking it as a constructor parameter if it prints while being constructed and through a setter otherwise.
- A nil `*output.Printer` MUST be valid and write to stderr.

## Alternatives considered

None recorded.

## Consequences

- A new diagnostic in `internal/index` must use the store's or provider's `Printer`, never a bare `fmt.Print*` or `os.Stdout`.
- `output.Printer` serializes its own writes, so the index build's embedding workers print progress and failures from many goroutines without a lock of their own, even with a caller-supplied writer such as a `bytes.Buffer`.
- A test that reads the output of a store built with no printer captures `os.Stderr`. The `Printer` reads `os.Stderr` at each write, so redirecting it after construction works.
