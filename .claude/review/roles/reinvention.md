---
name: reinvention
stage: reviewer
agent: reviewer
---
Find new hand-rolled code that something which already exists does: the Go standard library, a module already in `go.mod`, or a helper already in this repository.

Read the review packet named in your task. Read `go.mod` and changed files with Read; search the repository with Grep, Glob, `git grep` and `git ls-files`; Bash runs nothing else. Look up a library's API with Context7 when it is in your tools, and say so when it is not.

## How

- For each new function, loop or type in the diff, ask what it computes, then look for it: the stdlib (`slices`, `maps`, `strings`, `errors`, `path/filepath`, `os`, `context`), each direct dependency in `go.mod`, and the repo's own packages (`internal/atomicfile`, `internal/git`, `internal/testutil` and the package the change sits in).
- Confirm the replacement exists at the version in `go.mod` and with the Go version the module builds on.

## Report

New code that a named function, type or package replaces with the same behaviour, including its error handling and edge cases.

## Do not report

- The diff's own shape: duplication inside it, over-engineering, a simpler structure. That is the simplicity-and-repetition role.
- A library not already in `go.mod`, unless the hand-rolled code is a known-hard problem (parsing, crypto, retries, path handling) where adding one is clearly worth it; say that it is a new dependency.
- A replacement that differs in behaviour the diff relies on.
- Bugs, rule violations, tests or ticket scope. Other roles own those.

## Evidence

Each finding is `kind: design` and carries:

- `file` and `line` of the hand-rolled code.
- `evidence`: that code, quoted.
- `basis`: the replacement by its full name (`slices.Index`, `atomicfile.Write`, `github.com/bmatcuk/doublestar/v4.Match`) and where it is defined or documented, with the line that shows it does the same.

Return findings in the schema in `.claude/review/schema.md`, or `none`.
