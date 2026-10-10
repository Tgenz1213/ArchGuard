# Testing

## Rules

- **Drive tests from a table when the cases share a shape.** One loop, a named case per row. Write separate tests where setup or assertions differ.
- **Assert behavior, not printed wording.** Check exit codes, returned values, files written and state changed. Exact output is asserted only in `internal/output`'s own tests, where the layout is the behavior.
- **Keep live network out of unit tests.** Provider tests mock the backend with `net/http/httptest`. `test/e2e/e2e_test.go` builds and runs `cmd/archguard-e2e`, which wires `cli.Execute` to a `MockProvider`.
- **Separate tests by build tag, not by name.** A test that needs Docker carries `//go:build integration`, and every test in `test/e2e/` carries `//go:build e2e`. Everything else is a unit test and stays untagged, so `go test ./...` needs nothing installed. Tags apply to whole files: put a pure test that happens to sit beside a Docker-backed one in an untagged file, or it stops running in the unit job.
- **Use the test's context.** Pass `t.Context()` to every `exec.Command` and HTTP request (`noctx`). A test helper takes its context first, as [paradigm.md](paradigm.md) says.
- **Hold tests to the same rules as code.** Test helpers follow every page here, except where a page exempts test files.
- **Treat Docker-backed tests as real tests.** Postgres and pgvector integration tests use `testcontainers-go` and run with `go test -tags integration ./internal/index`. Start Docker and run them rather than reading "skipped" as a pass. A plain `go test ./...` does not run them, so run them yourself when you touch `internal/index`.
- **Run the race detector for anything concurrent.** CI runs `-race` on the unit, integration and e2e jobs.
- **Skip a retry backoff with a permanent error.** To skip `AnalyzeDrift`'s real retry backoff (about 14s), have the mock `ChatFunc` return `backoff.Permanent(err)`. An already-cancelled context does not do it: it ends `Engine.Run` with `context.Canceled` before any file is analyzed.
- **Add no tests in a refactor.** Existing tests that cover the moved behavior must pass unedited, and a test edited during a refactor is a sign the behavior changed.
