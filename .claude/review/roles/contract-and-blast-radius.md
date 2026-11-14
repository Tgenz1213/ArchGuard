---
name: contract-and-blast-radius
stage: reviewer
agent: reviewer
---
Find where the diff changes something other code, users or the GitHub Action depend on, without the change being deliberate and carried through.

Read the review packet named in your task. Read files with Read; search with Grep, Glob, `git grep` and `git ls-files`; read history with `git log` and `git show`. Bash runs nothing else, one command at a time.

## Check

- **CLI surface:** a flag added, removed, renamed or with a new default; an exit code changed; output on stdout that scripts parse; `check --ci` behaviour. `action.yml` runs `archguard check --ci`, so read it and confirm the Action still passes the flags and reads the exit codes the change leaves.
- **Config:** a key in `archguard.yaml` renamed, removed or re-typed, an environment override changed, a default changed. Existing config files must still load or fail with a clear error.
- **Exported APIs:** an exported identifier or interface method added, removed or re-typed. For a package in `.claude/rules/wide-packages.md`'s `paths`, list each such change and every importer it reaches (`git grep` for the import path), including `cmd/archguard-e2e` and test mocks.
- **History:** `git log --oneline -n 20 -- <file>` and `git show` on recent commits of each touched file. Report a change that undoes a recent fix or reverses a decision a commit message states.

## Report

A contract change that breaks a caller, the Action, an existing config or a documented behaviour, or that the diff makes without updating the callers, `README.md`, `action.yml` or the guidance that states it.

## Do not report

- Internal, unexported changes with no caller outside the package.
- Bugs inside the new behaviour (the breaker role), rule violations, duplication or ticket scope.
- A contract change the ticket asks for and the diff carries through everywhere.

## Evidence

Each finding is `kind: design` and carries:

- `file` and `line` of the changed contract.
- `evidence`: the old and new lines, quoted from the diff.
- `basis`: the affected caller, flag, exit code, config key, Action step or exported API, as `file:line` with its code or text quoted, and what it now gets that it did not before.

Return findings in the schema in `.claude/review/schema.md`, or `none`.
