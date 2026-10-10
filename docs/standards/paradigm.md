# Paradigm

Procedural orchestration over small structs and methods. A command is a sequence of calls that pass data along. State lives in structs, behavior in their methods, interfaces exist for substitution, composition replaces inheritance, and dependencies are passed in.

## Rules

- **Orchestrate procedurally.** A command function calls its steps in order and handles each step's error, as `execute` does for startup and `runCheck` for a check. Control flow lives in functions, not in a framework or an event system.
- **Keep data in structs and behavior in methods.** A type holds its own state and exposes what callers need (`Engine`, `Printer`, `LocalStore`). There is no class hierarchy.
- **Let a struct carry a command's state across steps.** When several steps share the same inputs, put them on a struct and make the steps methods, rather than passing six values to every function or one function holding all of them. A long parameter list hides what the steps share (the limit is in [functions.md](functions.md)).
- **Compose instead of inheriting.** Wrap or hold another type in a named field (`CompositeProvider` holds providers). Embed a type only to promote its whole method set on purpose.
- **Declare an interface where it is consumed, and keep it small.** `inference.Chatter` and `inference.Embedder` are one-role interfaces, and code that only chats takes a `Chatter`. A type gets an interface when a second implementation or a test double needs one, not before, because an interface next to its only implementation describes the implementation and constrains nothing.
- **Accept interfaces and return concrete types.** A constructor returns the struct, not an interface it satisfies.
- **Pass dependencies in.** They arrive as parameters, struct fields or injected functions (`ProviderFactories`), never through package-level mutable state, a singleton or `init()` setup. Global state makes order matter and lets tests interfere with each other.
- **Pass `context.Context` first and never store it in a struct.** It comes first even before `t` in a test helper (`revive` `context-as-argument`). The run's root context reaches every git subprocess and provider call (`docs/arch/0024-run-context-cancellation.md`).
- **Prefer a zero value that works.** A plain config field cannot tell "unset" from an explicit zero. A field that defaults to `true` is a `*bool`, because a `bool` cannot be set to `false` by omission and is silently wrong for anyone who does not write `false`. A field where zero is a legitimate value but the default is not, such as a threshold of `0.0`, is a pointer too (`*float64`): only nil takes the default and any explicit value, `0` or negative included, is honored. A field where zero has no meaning, such as a top-K of 0, stays a plain `int` that falls back to its default.
- **Use map, filter and small helpers where they are clearer than a loop.** A helper that names an operation earns its place; one that wraps a single call, or a chain of closures that hides the data flow, does not. Use the standard `slices` and `maps` packages before writing your own.
- **Leave out frameworks and magic.** No dependency-injection containers, reflection-driven wiring, or type hierarchies built for a design that has one case.

## Example

`cli.runCheck` passes `setup`, `opts` and two printers through a dozen steps. Those belong on one `checkRun` struct whose methods are the steps:

```go
run, err := newCheckRun(setup, opts, colors)
...
if err := run.loadIndex(ctx, store); err != nil {
```
