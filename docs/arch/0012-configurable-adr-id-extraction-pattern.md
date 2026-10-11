---
title: Configurable ADR ID extraction pattern
status: Accepted
scope: "internal/**"
---

# Configurable ADR ID extraction pattern

## Context

An ADR's ID comes from its filename, split on the first hyphen, which assumes the `NNNN-title.md` convention. A corpus named differently, such as `adr-1-use-postgres.md` and `adr-2-use-kafka.md`, gives every file the same ID (`adr`), so unrelated ADRs collide silently. See #154.

## Decision

`archguard.yaml` accepts an optional `analysis.adr_id_pattern` regex. It is compiled once at startup, so a bad regex fails fast with the config exit code instead of surfacing per file during a scan. A pattern with more than one capture group, named groups included, is also a config error, because the extractor would silently take group 1 whether or not it is the ID. Non-capturing `(?:...)` groups do not count.

When the pattern matches a filename, capture group 1 is the ID if the pattern has one, otherwise the whole match. When the pattern is unset, or does not match a given filename, that file falls back to the first-hyphen split. The fallback is per file, not per corpus, so a mixed-convention corpus still gets a sensible ID for every file.

When a pattern is set and matches none of the ADR files, the local provider prints a warning and carries on, since a pattern that matches nothing is almost always a typo. A pattern that matches some files is not warned about, so a mixed-convention corpus stays quiet.

The Confluence provider is unaffected: its IDs come from page IDs, not filenames.

## Consequences

- A corpus with another naming convention gets distinct, stable ADR IDs without renaming files.
- Existing configs are unaffected: with no pattern, IDs come from the first-hyphen split as before.
- A file that does not match the pattern falls back instead of erroring, so a pattern can cover only part of a corpus.
- A malformed regex is caught once at startup, consistent with other config validation, not per ADR during indexing or checking.
- `LocalStore`'s index hash includes each ADR's ID, so adding or changing `adr_id_pattern` triggers `check`'s automatic rebuild. No manual `archguard index` is needed.
