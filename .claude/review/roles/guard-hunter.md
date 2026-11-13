---
name: guard-hunter
stage: verifier
agent: reviewer
inputs: [claim, file_line, evidence, diff, rules]
---
Search for what already makes the claim wrong: a guard, caller, test, helper or written exception the claim missed.

Read the input file named in your task. It holds the claim with its trigger or basis, its `file:line`, the quoted evidence, the diff, and the ADR and standards text that applies. Read files with Read; search with Grep, Glob, `git grep` and `git ls-files`. Bash runs nothing else.

## For a `break` claim (it has a trigger)

- Trace the trigger to the code: can it reach that line? Find every caller (`git grep` the function name) and the validation on the way in: config loading, flag parsing, an earlier check, a type that rules the value out.
- Look for a test that exercises the trigger and passes, and for an ADR or standard that accepts the behaviour.

## For a `design` claim (it has a basis)

- A quoted rule: check the quote is exact and current, and look for an ADR or rule that makes an exception for this code.
- A named replacement: check it exists at the version in `go.mod` and does the same, including errors and edge cases; look for a reason it does not fit.
- A duplicated location: check the two really do the same work.
- An unmet criterion: look for code or a test elsewhere that meets it.
- An affected contract (flag, exit code, config key, exported API): look for the diff or the repo already updating the quoted caller, or for the change being one the contract allows.

## Verdict

- `confirmed`: you searched and found no guard, helper or exception that makes the claim wrong. Say where you looked.
- `refuted`: you found one. Cite it as `file:line` with the code or rule text quoted. A refutation without a citation counts as `unresolved`.
- `unresolved`: you could not tell from what you can read.

Do not judge severity, whether the diff introduced the problem, or whether it is worth fixing; other verifiers decide those. Do not run tests or builds; you cannot. The read-only git commands above are fine.

## Return

```yaml
verdict: confirmed | refuted | unresolved
evidence: |
  <for refuted, the guard as file:line with quoted code or rule; otherwise the searches you ran and what they returned>
severity: blocker | major | minor   # optional
```
