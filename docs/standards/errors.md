# Errors

## Rules

- **Wrap a cause with `%w`.** That keeps `errors.Is` and `errors.As` working, which exit-code mapping and tests depend on. Use `%v` only where callers must not match the cause, and say so in a one-line comment. Existing `%v` errors convert as the function they are in is refactored.
- **Write error strings in lower case with no trailing punctuation.** They are joined into longer messages (`revive` `error-strings`).
- **Add context, not noise.** Say what was being done (`failed to load rebuilt index: ...`), not that an error occurred, and do not wrap the same cause twice with the same words.
- **Return the error or handle it, never both.** Logging an error and then returning it reports it twice.
- **Report a deferred close error through a named result.** A `defer func() { err = f.Close() }()` reaches the caller only when `err` is a named result of the enclosing function. Assigned to a local variable, the error is silently dropped, and no linter catches it.
- **Never discard an error silently.** `errcheck` runs with `check-blank: true`, so `_ =` needs `//nolint:errcheck // <reason>` on the same line.
- **Name error values `errFoo` and error types `FooError`.** Exported functions return `error`, not a concrete error type (`revive` `error-naming`, `error-return`, `unexported-return`).
- **Map errors to exit codes in one place.** `internal/cli` turns errors into exit codes (`CLAUDE.md`); lower layers return errors and do not decide the process exit.
