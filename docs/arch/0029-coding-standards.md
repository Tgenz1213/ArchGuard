---
title: Coding standards live in docs/standards and are binding
status: Proposed
scope:
  - "**/*.go"
---

# Coding standards live in docs/standards and are binding

## Context

`runCheck` was 232 lines, `execute` 124 and `runInit` 93, and all of them passed lint: the linters in `.golangci.yml` check form (spacing, naming, error strings, parameter count) but not size or nesting. The same blocks had also been copied between commands, such as the ADR provider setup in `runCheck` and `runIndex`. Nothing written said what well-formed Go means here, so each change was judged by taste, and the comment rule alone was stated in three places.

## Decision

- **How code is written is documented in `docs/standards/`,** one page per concern (`paradigm`, `functions`, `errors`, `naming`, `formatting`, `comments`, `testing`), indexed by its `README.md`, which also fixes the order of precedence among this repository's docs and the external Go style guides. `docs/arch/` keeps recording why the design is what it is.
- **The paradigm is procedural orchestration over small structs and methods,** with interfaces for substitution, composition instead of inheritance, and dependencies passed in. Map, filter and helpers are welcome where they are clearer than a loop.
- **The standards are binding for new and changed code.** A rule changes by changing its page in the same pull request as the code that needs it.
- **Each rule has one home.** `CLAUDE.md` summarizes a rule in a line and points to its page rather than restating it.
- **The rules ArchGuard can check by reading code are repeated below,** because only `docs/arch/` is indexed.
- **`funlen`, `gocognit` and `nestif` are added to `.golangci.yml` once the existing functions that exceed the limits are refactored,** so lint carries no standing `//nolint` exemptions for them.

## Rules

- A function MUST NOT exceed 60 lines or 40 statements; split it by job instead.
- A block of more than a few lines MUST NOT be copied into a second place; extract it.
- Errors that wrap a cause MUST use `%w` unless callers must not match the cause.
- Code MUST NOT use package-level mutable state or `init()` functions for setup.
- An interface MUST be declared by the code that consumes it, not alongside its only implementation.

## Consequences

- Reviewers and the drift check have a written standard to point at instead of taste.
- The size limits split large functions into several named steps, so a reader follows `runCheck` by its call list; the cost is more small functions and a few more types.
- Existing `%v` errors convert to `%w` function by function as each is refactored, so the codebase is mixed until then.
- The size thresholds are a starting point. If a limit keeps flagging a readable function, change the limit in `docs/standards/functions.md` rather than scatter exemptions.
