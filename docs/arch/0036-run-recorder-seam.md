---
title: A run reports what it did through one recorder
status: Accepted
scope:
  - "internal/analysis/**"
  - "internal/cli/**"
  - "internal/telemetry/**"
---

# A run reports what it did through one recorder

## Context

#72 and #261 added token, call, cache, retry and per-file timing figures to `check`'s JSON `usage`. They were counted by hand in three places: metering decorators in `inference`, counters on the engine's per-file results, and a timer in `Engine.checkFile`. Someone who later wants these numbers in another system would have to find and touch each of them. See #262.

## Decision

- **`telemetry.Recorder` is the one place the engine and the provider boundary report what a run did.** It is an interface in `internal/telemetry` with one method per event, and a run calls it where the event happens.
- **Events are typed methods, named in the past tense for what happened.** `RequestSent(role, usage, err)` for every chat or embedding request, `CacheChecked(role, hit)` for every cache lookup, `RetriesSent(role, n)` for the retries behind one call, and `FileChecked(file, spent)` for each file the engine checked. A `Role` (`Judgment`, `Suggestion`, `Embedding`) says which kind of call an event belongs to. A new figure is a new method.
- **It is injected the way `Chat` and `Embed` are.** `Engine.Recorder` is a field, nil meaning a no-op, and `cli.ProviderFactories.Recorder` lets a caller of `Execute` add its own, nil by default. The provider boundary is recorded by `telemetry.Chatter` and `telemetry.Embedder`, decorators that `cli` wraps around the providers, one per role.
- **The JSON `usage` is built from a `telemetry.Tally`,** a recorder that keeps the totals in memory. `cli` always records into a `Tally` and, when a caller injected a recorder, into that one too.
- **A recorder never writes to stdout.** Stdout carries the report (`docs/arch/0028-check-report-and-log.md`); a recorder that prints writes to stderr or its own destination.

## Rules

- Code outside `internal/telemetry` that totals provider requests, token usage, cache lookups, retries or per-file time itself instead of calling `telemetry.Recorder`.
- A `telemetry.Recorder` implementation that writes to stdout.

## Alternatives considered

- **A Prometheus client, or OpenTelemetry on by default.** Both add a dependency and an exporter for a CLI that runs once and exits, with no consumer asking for either. An exporter can implement `Recorder` when one exists.
- **Generic `Count(name, n)` and `Time(name, d)` methods keyed by string.** They map onto exporters more directly, but a typo is a new metric instead of a compile error, and a test double has to know every name.
- **Passing the recorder through the context.** It hides a dependency the rest of the engine passes explicitly (`docs/arch/0034-token-usage-is-what-the-provider-reported.md`).
- **Keeping the counters where #72 and #261 put them.** That is the scattering this decision removes.

## Consequences

- The meters move from `inference` to `telemetry`, which imports `inference` for `Usage` and the provider interfaces; `inference` does not import `telemetry`.
- The per-stage `stages` totals stay in `stage.Telemetry`, which already gathers them in one object, and `Coverage` stays on the engine, since it is part of the verdict rather than a cost. Moving either is a separate change.
- `--dry-run`'s projected tokens are not an event, since nothing was sent, and stay on `Engine.Projection`.
