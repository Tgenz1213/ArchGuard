---
title: ADR screening rules
status: Accepted
scope: "internal/index/**"
---

# ADR screening rules

## Context

An ADR is prose, so anything screening code against it has to guess from the text what a violation looks like. The candidate scoring pipeline (docs/arch/0022) is meant to gain violation-screening scorers, which need a short, unambiguous statement of what breaks each ADR. Authors already write these: many ADRs carry a bullet list of MUST / MUST NOT statements under a `## Rules` heading. See #203 (part of #196); screening with rules is #204.

## Decision

An ADR may declare rules. A `Rule` (`internal/index/rules.go`) is a `Statement` plus optional `Violating` and `Compliant` example lists, and an ADR carries them as `ADR.Rules`. Rules can be written in two places:

- **Frontmatter**, under the canonical key `rules` (remappable through `analysis.frontmatter_mappings`, docs/arch/0021): a list whose items are either plain statement strings or mappings with `statement` and optional `violating`/`compliant`, each a string or a list of strings.
- **A body section**, the list items under the heading named by `analysis.rules_heading` (default `Rules`, `DefaultRulesHeading`), matched case-insensitively at any heading level and ending at the next heading of the same or a higher level. Each top-level list item's first paragraph is one statement; nested lists and surrounding prose are ignored, so body rules carry no examples. The body is parsed with goldmark's CommonMark AST, so code fences, setext headings and loose or tight lists behave as Markdown defines them.

Valid frontmatter rules win. Absent, null or empty frontmatter rules fall through to the body section. Present but malformed frontmatter rules do not fall back to the body, since the author clearly meant the frontmatter and silently switching sources would hide the mistake. A body section with no list items means no rules, not an error; a list item with no text of its own is.

Malformed rules never fail the ADR. The private `parseADR` returns them as a separate `rulesErr` beside the fatal `err`; `ADR` carries no error state. `LocalProvider` and `ConfluenceProvider` handle `rulesErr` at the parse call for accepted-status ADRs: they print a warning through their writer and append `MalformedRules{RelPath, Reason}` to `FetchStats`, which `CompositeProvider` merges and `summarizeCorpus` copies into `IndexSummary`, so `archguard index` lists them under "Rules ignored (malformed)". Confluence parses frontmatter from tag-stripped page text, but reads the body section from the converted Markdown that becomes `ADR.Content`, because tag stripping loses headings and list structure.

Rules persist with the ADR. `LocalStore` stores them in `index.json` (omitted when empty), and `CalculateHash` mixes in an ADR's JSON-encoded rules only when it has some, so a corpus without rules keeps its exact hash while a rules edit makes `archguard check` rebuild. `PgStore` adds a nullable JSON `rules` TEXT column through `ensureSchema`, writes it on upsert and metadata sync, and reads it in `Search` and `ScopedADRs`; a rules change with unchanged `Content`/`Title`/`Status` takes the metadata-sync path, not a re-embed.

Rejected alternatives:

- A hand-rolled Markdown line parser: it re-implements fences, indentation and heading rules that a CommonMark parser already gets right.
- `Violating:`/`Compliant:` labelled sub-bullets in the body: an invented convention no real ADR uses; examples stay in frontmatter.
- A `RulesError` field on `ADR`: transient per-run diagnostics don't belong on persisted data; the problem is returned and handled where it occurs.
- Hashing rules unconditionally: every existing local index would rebuild once on upgrade for no reason.

## Consequences

- ADR authors state what a violation looks like once, in the form they already use, and screening scorers get it without re-reading prose.
- ADRs without rules load, hash and index exactly as before, and ADR content sent to the LLM is unchanged.
- Rules do not yet change what `check` judges or reports; nothing consumes them until #204.
- The default heading `Rules` picks up existing `## Rules` sections on upgrade. A section that uses that heading for something else yields rules or a malformed-section warning on every `index`/`check`; `analysis.rules_heading` redirects it.
- Adding `rules` to the canonical frontmatter fields makes an existing `frontmatter_mappings` entry that maps another field onto the key `rules` a startup `ExitConfig`. The collision error names the fix: remap `rules` itself.
- Changing `analysis.rules_heading` or remapping `rules` is detected by `LocalStore`'s hash, because the resolved rules are hashed, but not by `PgStore`, which needs an explicit `archguard index`.

## Rules

- An ADR MUST NOT be skipped or fail to load because its rules are malformed; only its rules are dropped, and the problem is reported.
- Malformed-rules problems MUST be returned to the caller and handled there, not stored on the `ADR` struct.
- Body-section rules MUST be read with a Markdown parser, not hand-rolled line scanning.
- Valid frontmatter rules MUST take precedence over body-section rules, and malformed frontmatter rules MUST NOT fall back to the body.
- `LocalStore.CalculateHash` MUST NOT change the hash of an ADR that has no rules.
