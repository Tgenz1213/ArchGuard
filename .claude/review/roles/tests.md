---
name: tests
stage: reviewer
agent: prober
---
Find new or changed tests that would pass without the change they test, or that assert nothing about its behaviour.

Set up your worktree as your task says (apply the patch with `git apply`, or `git checkout --detach` the ref), then read the review packet named in your task.

## How

For each new or changed test function in the diff:

1. Run it: `go test ./<pkg> -run <Name> -count=1 -v`. It must pass first; if it fails, report that as the finding. `-run` matches any test whose name contains `<Name>`, so read which tests ran; the hook refuses `$`, so anchors cannot be used.
2. Revert the production file it covers to the base, keeping the test file: `git restore <file>` when you applied a patch, `git restore --source=<base> -- <file>` when the change is already in your checkout (the packet names the base). Where the file has other changes the test needs to compile, mutate the behaviour the test claims to check instead, with Edit: flip a condition, return early, drop an error, change a constant.
3. Run it again. A test that still passes does not guard the change.
4. Put the change back before the next test: `git apply --include=<file> <patch>`, or `git restore --source=HEAD -- <file>`.

Bash runs only `go test`, `go build`, `go vet`, `go run` and `git apply`, `restore`, `diff`, `log`, `show`, `status`, `grep`, `ls-files`, one command at a time. Run unit tests only; a test that needs Docker, Postgres, the network or the e2e tag is not run, and you say so in one line.

## Report

- A test that passes with the change reverted or mutated.
- A test that only executes the code: no assertion on a returned value, error, file written, exit code or state changed.
- A test that asserts printed wording outside `internal/output`, where `docs/standards/testing.md` asks for behaviour.

## Do not report

- Missing tests for untested code paths, unless the ticket's criteria name the test. That is ticket-and-necessity.
- Bugs in the production code (the breaker role), style or naming.

## Evidence

Each finding is `kind: break` and carries:

- `file` and `line` of the test function.
- `evidence`: the assertion lines, quoted, or the test body when it has none.
- `trigger`: the revert or mutation you made, as a diff.
- `repro`: the command and its output, before and after the mutation.

Return findings in the schema in `.claude/review/schema.md`, or `none`. Then reset your worktree as your agent instructions say.
