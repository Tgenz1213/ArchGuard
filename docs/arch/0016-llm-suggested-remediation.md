---
title: "LLM-suggested remediation is a second, opt-in call"
status: "Accepted"
scope: "internal/**"
---

# LLM-suggested remediation is a second, opt-in call

## Context

Issue #73 asked for a short remediation pointer with each violation, since knowing what broke and why does not tell the reader what to do. One option was to add "and suggest a fix" to the violation-judgment prompt: one call, so cheaper. The other was a second call, made only after a violation is confirmed.

The judgment prompt is deliberately narrow and false by default, to avoid false positives. Asking it for a fix as well risks biasing the model toward finding a violation, just to have something to suggest.

A suggestion is unverifiable: ArchGuard cannot confirm that following it resolves the violation. It also costs an extra LLM call per violation. So it never runs unless a user or CI job opts in.

## Decision

- The suggestion is a separate call with its own prompt. It runs only after the judgment call confirms a violation, and only for a newly reported one: not under `--update-baseline`, and not for a baselined or suppressed violation.
- It is off by default and turned on by `check --suggest-fixes`.
- A computed suggestion is cached, so a later run of the same file and ADR reuses it without another call. `docs/arch/0017-suggestion-cache-key-namespace.md` amends how: suggestions live in their own cache namespace and are never merged into the judgment result.
- Output labels the suggestion as unverified, and it is documented as an LLM-generated pointer, never a guaranteed fix.

## Consequences

- Positive: nothing changes in behaviour or cost without `--suggest-fixes`, and the judgment prompt's accuracy is untouched because its text is unchanged.
- Positive: a suggestion is paid for at most once per cache key (see `docs/arch/0017-suggestion-cache-key-namespace.md`), even when `--suggest-fixes` is toggled on and off.
- Negative: a violation cached by an earlier run without `--suggest-fixes` costs one extra call the first time the flag is added. This is expected and cheap.
- Negative: with the flag on, each violation takes a second round trip, which roughly doubles check latency for a file with many violations. This is acceptable because it is opt-in.
