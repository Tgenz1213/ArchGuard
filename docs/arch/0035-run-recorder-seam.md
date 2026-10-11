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

#72 and #261 added token, call, cache, retry and per-file timing figures to `check`'s JSON `usage`. They were counted by hand in three places: metering decorators around the providers, counters on the engine's per-file results, and a timer in the engine. Someone who later wants these numbers in another system would have to find and touch each of them. See #262.

## Decision

- **`telemetry.Recorder` is the one place the engine and the provider boundary report what a run did.** It has a single method that takes an event, and a run calls it where the event happens.
- **Events are typed structs, named in the past tense for what happened.** Each carries the kind of call it belongs to (judgment, suggestion or embedding). A new figure is a new event type, and a recorder ignores the types it does not handle, so adding one changes no recorder or test double.
- **It is injected the way `Chat` and `Embed` are.** It is optional on the engine, and a caller of `cli.Execute` can add its own. The provider boundary is recorded by decorators around the providers, one per kind of call.
- **The JSON `usage` is built from a recorder that keeps the totals in memory.** It records every run, alongside any recorder a caller injected.
- **A recorder never writes to stdout.** Stdout carries the report (`docs/arch/0028-check-report-and-log.md`); a recorder that prints writes to stderr or its own destination.

## Rules

- Code outside `internal/telemetry` that totals provider requests, token usage, cache lookups, retries or per-file time itself instead of calling `telemetry.Recorder`.
- A `telemetry.Recorder` implementation that writes to stdout.

## Alternatives considered

- **A Prometheus client, or OpenTelemetry on by default.** Both add a dependency and an exporter for a CLI that runs once and exits, with no consumer asking for either. An exporter can implement `Recorder` when one exists.
- **One method per event.** It is type-safe, but every new event changes the interface and breaks every recorder and test double at compile time.
- **Generic `Count(name, n)` and `Time(name, d)` methods keyed by string.** They map onto exporters more directly, but a typo is a new metric instead of a compile error.
- **Passing the recorder through the context.** It hides a dependency the rest of the engine passes explicitly (`docs/arch/0034-token-usage-is-what-the-provider-reported.md`).
- **Keeping the counters where #72 and #261 put them.** That is the scattering this decision removes.

## Consequences

- The meters live in `telemetry`, which imports `inference`; `inference` does not import `telemetry`.
- The per-stage totals and the run's coverage stay where they are: the stage totals are already gathered in one object, and coverage is part of the verdict rather than a cost. Moving either is a separate change.
- `--dry-run`'s projected tokens are not an event, since nothing was sent.
