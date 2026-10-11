---
title: ADR screening rules
status: Accepted
scope: "internal/index/**"
---

# ADR screening rules

## Context

An ADR is prose, so anything screening code against it has to guess from the text what a violation looks like. The candidate scoring pipeline (docs/arch/0022) is meant to gain violation-screening scorers, which need a short, unambiguous statement of what breaks each ADR. Authors already write these: many ADRs carry a bullet list of MUST / MUST NOT statements under a `## Rules` heading. See #203 (part of #196); screening with rules is #204.

## Decision

An ADR may declare rules. A rule is a statement plus optional violating and compliant examples. Rules can be written in two places:

- **Frontmatter**, under the canonical key `rules` (remappable through `analysis.frontmatter_mappings`, docs/arch/0021): a list of plain statements, or of mappings with a `statement` and optional `violating` and `compliant` examples.
- **A body section**: the list items under the heading named by `analysis.rules_heading`, which defaults to `Rules`. The heading matches on its plain text, case-insensitively, at any level, and the section ends at the next heading of the same or a higher level. Each top-level list item is one statement. Nested lists and surrounding prose are ignored, so body rules carry no examples. The body is read with a CommonMark parser, so code fences, headings and lists behave as Markdown defines them.

Valid frontmatter rules win. Absent, null or empty frontmatter rules fall through to the body section. Malformed frontmatter rules do not fall back to the body, since the author clearly meant the frontmatter and silently switching sources would hide the mistake. A body section with no list items means no rules, not an error; a list item with no text of its own is an error.

Malformed rules never fail the ADR. The parser returns the problem beside the ADR, and the ADR itself carries no error state. The ADR provider warns, and `archguard index` lists the ADR under "Rules ignored (malformed)". Confluence reads frontmatter from tag-stripped page text but reads the body section from the converted Markdown, because tag stripping loses headings and list structure.

Rules persist with the ADR in both vector stores. The local index hash covers an ADR's rules only when it has some, so a corpus without rules keeps its exact hash while a rules edit makes `archguard check` rebuild. In Postgres, a rules change with unchanged content, title and status is synced without re-embedding.

Rejected alternatives:

- A hand-rolled Markdown line parser: it re-implements fences, indentation and heading rules that a CommonMark parser already gets right.
- `Violating:`/`Compliant:` labelled sub-bullets in the body: an invented convention no real ADR uses; examples stay in frontmatter.
- A `RulesError` field on `ADR`: transient per-run diagnostics don't belong on persisted data; the problem is returned and handled where it occurs.
- Hashing rules unconditionally: every existing local index would rebuild once on upgrade for no reason.

## Consequences

- ADR authors state what a violation looks like once, in the form they already use, and screening scorers get it without re-reading prose.
- ADRs without rules load, hash and index exactly as before, and ADR content sent to the LLM is unchanged.
- Rules alone do not change what `check` judges or reports; screening consumes them in #204.
- The default heading `Rules` picks up existing `## Rules` sections on upgrade. A section that uses that heading for something else yields rules or a malformed-section warning on every `index`/`check`; `analysis.rules_heading` redirects it.
- Adding `rules` to the canonical frontmatter fields makes an existing `frontmatter_mappings` entry that maps another field onto the key `rules` a startup config error. The collision error names the fix: remap `rules` itself.
- Changing `analysis.rules_heading` or remapping `rules` is detected by the local index hash, because the resolved rules are hashed, but not by Postgres, which needs an explicit `archguard index`.

## Rules

- An ADR MUST NOT be skipped or fail to load because its rules are malformed; only its rules are dropped, and the problem is reported.
- Malformed-rules problems MUST be returned to the caller and handled there, not stored on the `ADR` struct.
- Body-section rules MUST be read with a Markdown parser, not hand-rolled line scanning.
- Valid frontmatter rules MUST take precedence over body-section rules, and malformed frontmatter rules MUST NOT fall back to the body.
- `LocalStore.CalculateHash` MUST NOT change the hash of an ADR that has no rules.
