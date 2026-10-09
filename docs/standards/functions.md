# Functions

## Rules

- **Keep a function to 60 lines and 40 statements, a cognitive complexity of at most 30 and no `if` block of nesting complexity 4 or more.** That is `funlen`, `gocognit` and `nestif` in `.golangci.yml`, with no `//nolint` exemptions; test files are not checked, since a table-driven test runs long by design. A reader should hold a function in their head in one pass.
- **Split by job and name each piece for what it does.** `loadIndex`, `loadBaseline` and `finishUpdateBaseline` say what happens; `part2` and `helper` do not. A function that runs one step after another reads as a list of names.
- **Keep orchestrators short and flat.** A command function calls its steps in order and handles each step's error. It does no work of its own beyond that.
- **Extract a block the second time it appears.** A block of more than a few lines that appears twice is extracted once and reused. `runCheck` and `runIndex` both built the same ADR provider, which is one `newADRProvider`. `runInit` repeated one prompt block four times, which is one `prompt` helper.
- **Take at most five parameters, not counting the receiver.** This holds for tests too, and a longer list becomes a struct (`revive` `argument-limit`).
- **Return early.** Handle the error or the special case and return, so the main path is not nested (`revive` `early-return`, `indent-error-flow`, `superfluous-else`).
- **Stay at one level of abstraction per function.** Do not mix decisions about what to do with the details of how a file is read or a string is built.
- **Do not split just to reach the number.** A flat function made of independent steps, such as one that prints each section of a report, may run longer when splitting would only add indirection. Judge by whether a reader can follow it, and change the threshold here if it keeps flagging a readable function.
