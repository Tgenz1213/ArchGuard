# Formatting

## Rules

- **Use `gofmt` as the formatter.** Nothing is hand-aligned or hand-wrapped against it. On this Windows checkout, strip `\r` before running `gofmt -l`, or it flags every file.
- **Follow `wsl_v5`'s `if` and `after-block` rules for blank lines.** Put a blank line before an `if` unless it checks the assignment right above it, and after a closing `}` before the next statement. `golangci-lint run --fix --enable-only wsl_v5` fixes spacing.
- **Use blank lines to separate steps, not every line.** Group related statements; a blank line marks a change of step. A function with no blank lines, or one after every line, is hard to scan.
- **Group imports in two blocks.** The standard library comes first, then everything else (this module's packages included), each sorted by `gofmt`. Dot imports are banned, and blank imports are allowed only in `main` and for drivers (`revive` `dot-imports`, `blank-imports`).
- **Declare variables where first used.** Use `var x T` only for a zero value you rely on (`revive` `var-declaration`).
- **Wrap a long call one argument per line.** Give each argument a trailing comma and put the closing paren on its own line. Do not leave half the arguments on the first line.

  ```go
  confluenceProvider := index.NewConfluenceProvider(
  	cfg.Analysis.Confluence.Domain,
  	cfg.Analysis.Confluence.SpaceID,
  	cfg.Analysis.Confluence.Username,
  	cfg.Analysis.Confluence.Token,
  	cfg.Analysis.AcceptedStatuses,
  )
  ```

- **Put each link of a long call chain on its own line.** A chain of more than two links starts each line with the `.`. A chain that needs more than three lines is a sign to name an intermediate value instead.
- **Group related declarations.** Use `const (...)` and `var (...)` blocks, and keep each block to one concern.
- **Use the modern forms.** Write `x++` and `any`, not `x += 1` and `interface{}` (`revive` `increment-decrement`, `use-any`).
- **End every file with a newline and leave no trailing whitespace.**
