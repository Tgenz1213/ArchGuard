---
title: Go code follows the conventions in docs/standards
status: Accepted
scope:
  - "**/*.go"
---

# Go code follows the conventions in docs/standards

## Context

`runCheck` was 232 lines, `execute` 124 and `runInit` 93, and all of them passed lint: the linters in `.golangci.yml` check form (spacing, naming, error strings, parameter count) but not size or nesting. The same blocks had also been copied between commands, such as the ADR provider setup in `runCheck` and `runIndex`. Nothing written said what well-formed Go means here, so each change was judged by taste, and the comment rule alone was stated in three places.

## Decision

- **Go code in this repository follows the conventions in `docs/standards/`.** The pages hold the rules, one per concern, and its `README.md` indexes them and fixes the order of precedence among this repository's docs and the external Go style guides. This ADR records the decision to follow them and holds no rules of its own.
- **The standards bind new and changed code.** A rule changes by changing its page in the same pull request as the code that needs it; this ADR stays valid.
- **Each rule has one home.** `CLAUDE.md` summarizes a rule in a line and points to its page rather than restating it.
- **Lint enforces what it can see.** The rest is held in review. Function-size and complexity limits (`funlen`, `gocognit`, `nestif`) are enabled in `.golangci.yml` at the thresholds in `docs/standards/functions.md`, for non-test code, so lint carries no standing `//nolint` exemptions for them.

## Rules

- Go code MUST follow the conventions in `docs/standards/`.

## Consequences

- Reviewers and the drift check point at a written standard instead of taste.
- The rules can change often without amending an ADR, because the decision is only that they are followed.
- Code that predates a standard is brought into line in follow-up changes, so the codebase is mixed until they land.
