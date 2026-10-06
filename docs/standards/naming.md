# Naming

## Rules

- **Use at most two single-letter names per scope.** Name the rest. `i`, `n`, `ok`, `err` and a receiver are the usual two; a third is `cfg`, `store` or `file`.
- **Make a name say what a thing is or does.** Functions are verbs (`loadBaseline`) and types and variables are nouns (`checkRun`). Avoid `data`, `info`, `util`, `handle` and `process`.
- **Scale the name to the scope.** A loop index is `i`; a value used across forty lines is not `v`.
- **Keep receivers to one or two letters.** Use the same one on every method of a type (`revive` `receiver-naming`).
- **Keep initialisms in one case.** Write `ID`, `URL`, `ADR`, `JSON` and `HTTP`, so `adrID` and `ADRPath`, not `adrId` or `AdrPath` (`revive` `var-naming`).
- **Name packages short, lower case, single-word and without stutter.** `index.Store`, not `index.IndexStore`. There are no `util`, `common` or `helpers` packages.
- **Do not shadow builtins or imports.** A local named `len` or `index` hides the real one (`revive` `redefines-builtin-id`, `import-shadowing`).
- **Make a boolean read as a question or a switch.** `isDriftError`, `jsonOutput` and `updateBaseline` are true when the thing is on.
- **Name a file for the job it holds.** In `internal/cli` that is `check.go`, `init.go` and `providers.go`.
