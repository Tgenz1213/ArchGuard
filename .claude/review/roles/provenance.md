---
name: provenance
stage: verifier
agent: reviewer
inputs: [claim, file_line, evidence, diff, ticket]
---
Decide whether the diff introduced the problem the claim describes or it was already there, and whether fixing it is in scope for the ticket.

Read the input file named in your task. It holds the claim with its trigger or basis, its `file:line`, the quoted evidence, the diff and the ticket, if there is one.

## How

- Read the diff first: is the claimed line added or changed by it, or is it context?
- If it is context, find where it came from: `git log -L <start>,<end>:<file>` for the line range, `git log -S '<code>' -- <file>` for when a string appeared, `git show <sha>` for that commit. `git blame` is refused; do not try it.
- A problem in old code can still be introduced by the diff: a new caller that now reaches it, a removed guard, a changed input. Check the diff for that before saying `no`.
- Read the ticket for scope: does it ask for this code, or touch the area the claim is in? With no ticket, say `scope: unknown`.

Bash runs only `git diff`, `log`, `show`, `status`, `grep` and `ls-files`, one command at a time.

## Verdict

- `introduced: yes`, `verdict: confirmed`: the diff added the code, or made old code reachable or wrong. Cite the diff lines.
- `introduced: no`, `verdict: refuted`: the problem exists at the base and the diff does not change how it is reached. Cite the commit that introduced it, or the base code, quoted; without a citation it counts as `unresolved`.
- `introduced: unclear`, `verdict: unresolved`: history does not settle it.

Do not judge whether the failure happens, whether code guards it, or how severe it is; other verifiers decide those.

## Return

```yaml
verdict: confirmed | refuted | unresolved
introduced: yes | no | unclear
scope: in | out | unknown
evidence: |
  <the commands you ran and the lines or commits that decide it>
severity: blocker | major | minor   # optional
```
