## Summary

What changed and why (1-3 sentences).

## Related issue

Closes #

## In scope

## Out of scope

## Architectural notes

Any new invariant, rejected alternative, or cross-cutting constraint worth recording. Link the ADR under `docs/arch/` if one was added.

## Follow-ups

Known gaps or deferred work, with an issue link if one exists.

## Checklist

- [ ] Title follows Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `build(deps):`, etc.)
- [ ] All acceptance criteria from the linked issue are met
- [ ] `go test -race -cover ./...` passes
- [ ] `golangci-lint run --timeout=5m` is clean
- [ ] `CLAUDE.md` updated if this changes build/test commands, a cross-package interface, or adds a footgun
- [ ] A new ADR added under `docs/arch/` if this embodies an architecturally-significant decision
- [ ] Comments follow the 2-line-max, WHY-only convention
