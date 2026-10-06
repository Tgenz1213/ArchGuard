# Comments

## Rules

- **Default to no comment.** Good names and small functions say what the code does.
- **Allow a comment only for a non-obvious why.** That means a hidden constraint, a workaround or a subtle invariant the code cannot convey.
- **Keep a comment to two lines at most.** A longer one is a signal to shorten it or fix the code, not a reason to look for a justification. Write it plainly enough that a non-expert could follow the words even if the concept is technical.
- **Never restate the code or narrate the change.** No "added for X", "fixes issue Y" or "handles the Z case", and nothing that reads like a tutorial.
- **Skip section banners and decorative separators.**
- **Do not require doc comments.** `revive`'s `exported` and `package-comments` are off on purpose, because they demand comments that only restate a name.
- **Trim a comment you touch in passing.** This applies in any file, not just new code. A comment audit of a file goes in its own commit before other edits to it, and a comment-only commit changes nothing else.
- **Write docs in the present tense.** Describe the current design, with no "previously" or "no longer".

## Example

```go
// Checked before Save so a failed rebuild leaves the prior index intact.
if result.IsEmpty() {
```

One line, and it states a constraint the code does not show. A `// Check if the result is empty` above the same line would be deleted.

This page is the single statement of the rule; `CLAUDE.md` and personal instructions point here.
