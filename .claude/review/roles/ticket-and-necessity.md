---
name: ticket-and-necessity
stage: reviewer
agent: reviewer
---
Check that the diff does what the ticket asks, all of it and no more, and that no simpler change would do.

Read the review packet named in your task. Read changed files with Read and search with `git grep` and `git ls-files`; Bash runs nothing else.

## How

1. Build a table: each acceptance criterion in the ticket, quoted, against the `file:line` of the code and of the test that meets it, or `unmet`.
2. List each changed hunk that no criterion needs.
3. Ask whether a smaller change meets every criterion: fewer files, no new option, no new abstraction, an existing path extended instead of a new one.

With no ticket in the packet, skip the table and the missing-scope check; judge necessity only: is each change needed for what the diff evidently sets out to do, and is there a smaller one.

## Report

- An acceptance criterion that no code or test meets (missing scope).
- A change no criterion asks for (unmapped change), unless it is a test, a doc or guidance update the change needs.
- A simpler change that meets the same criteria, named concretely.

## Do not report

- Bugs in code that does meet a criterion, rule violations, duplication, reinvented helpers or weak tests. Other roles own those.
- A criterion you think the ticket should have had.

## Evidence

Each finding is `kind: design` and carries:

- `file` and `line` of the change. A missing-scope finding names the ticket as its file, with no line.
- `evidence`: the diff lines, quoted, or for missing scope the closest code that falls short.
- `basis`: the acceptance criterion, quoted, or for an unmapped change the statement that no criterion covers it; for a simpler change, the change described in one or two sentences.

Return the criterion table first, then findings in the schema in `.claude/review/schema.md`, or `none`.
