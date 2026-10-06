# Coding standards

How Go code is written in this repository. `docs/arch/` records why the design is the way it is; these pages record how code is written. Read the page for the thing you are about to write.

| Page | Covers |
|---|---|
| [paradigm.md](paradigm.md) | Procedural Go, state in structs, where interfaces live, no globals |
| [functions.md](functions.md) | Size and complexity limits, extraction, repetition, parameters |
| [errors.md](errors.md) | Wrapping, error strings, early returns, discarded errors |
| [naming.md](naming.md) | Identifiers, single-letter names, receivers, package names |
| [formatting.md](formatting.md) | `gofmt`, blank-line rules, imports, wrapping calls and chains |
| [comments.md](comments.md) | When a comment is allowed and how long it may be |
| [testing.md](testing.md) | Table-driven tests, mocks, what a test may assert |

## Precedence

1. `docs/arch/0029-coding-standards.md` and these pages.
2. `CLAUDE.md`.
3. [Effective Go](https://go.dev/doc/effective_go)
4. [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
5. [Google Go Style Guide](https://google.github.io/styleguide/go/) (decisions and best practices)
6. [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

A later source applies only where an earlier one is silent. `.golangci.yml` is the part of this that a tool checks; a rule here that the linter cannot see is held in review.

## Changing a standard

Change the page in the same pull request as the code that needs it, and say why in the description. A rule that keeps needing an exemption is the wrong rule: change the rule instead of adding exemptions. A rule ArchGuard can check by reading code is also written in `docs/arch/0029-coding-standards.md`, because only `docs/arch/` is indexed.

## Page shape

Each page is a `## Rules` list. A rule opens with a bold sentence that ends in a period, followed by its reason and the `revive` or other lint rule that enforces it, if there is one. An `## Example` from this repository follows where it helps.
