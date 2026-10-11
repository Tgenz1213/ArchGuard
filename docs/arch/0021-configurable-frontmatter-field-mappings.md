---
title: Configurable frontmatter field-name mappings
status: Accepted
scope: "internal/**"
---

# Configurable frontmatter field-name mappings

## Context

ADR frontmatter is read from fixed field names. Teams adopting ArchGuard often have an ADR corpus, MADR-flavoured or in a house style, whose frontmatter uses different names. Renaming every field in every ADR is a real adoption tax. It is the same class of problem `analysis.adr_id_pattern` (docs/arch/0012) solves for filename-based IDs. See #191.

## Decision

`analysis.frontmatter_mappings` maps a canonical field name to the YAML key the project uses. The canonical fields are the ones ArchGuard reads from frontmatter, including `rules` (docs/arch/0023).

The mapping is validated once at startup, and two mistakes fail fast with the config exit code instead of producing silently wrong ADR data later:

- An unknown canonical field name, such as the typo `scop`.
- A collision, where two canonical fields resolve to the same YAML key: two mapped fields pointing at one key, or a mapped key equal to an unmapped field's own canonical key.

An unset or empty mapping leaves parsing exactly as it is without the feature. A field left unmapped reads its canonical key. A field whose key is absent stays at its zero value, so `similarity_threshold`'s distinction between unset and an explicit zero (docs/arch/0011) holds for a remapped key as it does for the default one.

The mapping is applied in the shared ADR parser, so it works the same for every ADR source, local files and Confluence alike.

## Consequences

- A corpus with non-canonical field names adopts ArchGuard by mapping only the fields that differ, without renaming ADR files or replacing the whole frontmatter schema.
- Existing configs are unaffected.
- An unmapped field behaves the same even when other fields in the corpus are remapped.
- Misconfiguration is caught once at startup, not per ADR during indexing or checking.
- As with any change to what a field resolves to (see docs/arch/0008 on hash-based rebuilds), enabling or changing `frontmatter_mappings` on an indexed corpus is not detected. The local index hash does not cover the title, status, scope or similarity threshold, and the Postgres hash is constant. Remapping `rules` is the exception for the local index, since the resolved rules are hashed. A corpus that adopts or changes `frontmatter_mappings` should run `archguard index` explicitly.
