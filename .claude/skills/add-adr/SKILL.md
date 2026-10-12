---
name: add-adr
description: Write a new Architectural Decision Record in docs/arch/, or supersede one. Use when a change makes or reverses a design decision, or a PR is architecturally significant.
---

# Add an ADR

What an ADR may hold is in `docs/standards/guidance.md`; how CI reads one is in `.claude/rules/adr-docs.md`. Read both first.

1. **Check it is a decision.** A change in behaviour, a default or a call flow is not one; stop here if that is all it is.
2. **Pick the number.** Take the highest number in `docs/arch/` on `origin/main`, then check open PRs for an ADR that already claims the next one. In a cloud session, `gh` is not installed; list open PRs with the GitHub connector and look at their changed files under `docs/arch/`. Use the next number nobody holds, and re-check it just before the PR opens.
3. **Copy `docs/ADR_TEMPLATE.md`** to `docs/arch/NNNN-<short-kebab-title>.md`. Keep every `##` heading, in order.
4. **Fill it in.**
   - One decision per ADR. A second decision gets its own ADR.
   - `scope` names the files the decision governs, as narrowly as it can.
   - Each `## Rules` bullet is one MUST or MUST NOT statement a model can check against a single file in `scope`. Write "None." when no file could break the decision.
   - Set `status` to a value in `analysis.accepted_statuses` in `archguard.yaml`, or the ADR is ignored.
5. **Superseding an ADR.** Write the new ADR as above and name the old one in its Context. In the old ADR change only `status`, to `Superseded`; never edit its text.
6. **Check it.** Run `go test ./test/guidance` and fix what fails.
7. **Link it** from the PR description.
