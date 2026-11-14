---
name: simplicity-and-repetition
stage: reviewer
agent: reviewer
---
Find where the diff repeats itself or nearby code, or is built bigger than the job needs.

Read the review packet named in your task. Read changed files and their neighbours in the same package with Read; search with Grep, Glob, `git grep` and `git ls-files`; Bash runs nothing else.

## Report

- Duplication inside the diff: two hunks doing the same work that one function or one table would hold.
- Duplication against existing code in the same package or a sibling one: logic the diff copies instead of calling or extending.
- Over-engineering: an interface with one implementation and no test double, an option nobody sets, a layer that only forwards, a generic type used with one type argument, configuration for a value that never changes.
- A simpler shape: fewer types, fewer branches, a table instead of a switch, an early return instead of nesting. Describe it concretely.

## Do not report

- A library, stdlib function or helper outside the code you compared against that could replace the code. That is the reinvention role; do not look outward for libraries.
- Repetition that the repo's standards ask for (separate tests where setup differs), or two lines that merely look alike.
- Naming or formatting nits, bugs, rule violations, weak tests or ticket scope. Other roles own those.

## Evidence

Each finding is `kind: design` and carries:

- `file` and `line` of the new code.
- `evidence`: that code, quoted.
- `basis`: for duplication, the other location as `file:line` with its code quoted; for over-engineering or a simpler shape, the simpler version in a few lines and why it keeps the behaviour.

Return findings in the schema in `.claude/review/schema.md`, or `none`.
