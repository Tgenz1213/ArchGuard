---
title: Configurable frontmatter field-name mappings
status: Accepted
scope: "internal/**"
---

# Configurable frontmatter field-name mappings

## Context

`FrontMatter` decodes ADR YAML frontmatter into a struct with fixed `yaml:"..."` tags: `title`, `status`, `scope`, `similarity_threshold`. Teams adopting ArchGuard often already have an existing ADR corpus (MADR-flavored, or their own house style) whose frontmatter uses different field names. Today the only option is renaming every field in every existing ADR file, which is a real adoption tax -- the same class of problem `analysis.adr_id_pattern` (docs/arch/0012) already solves for filename-based ID extraction. See #191.

## Decision

`config.Analysis` gains an optional `FrontmatterMappings map[string]string` field (`frontmatter_mappings` in YAML), keyed by canonical field name (`title`, `status`, `scope`, `similarity_threshold`) with the project's actual YAML key as the value. `internal/cli.validateFrontmatterMappings` validates it once at startup, right after `compileADRIDPattern`, so two classes of mistake fail fast with `ExitConfig` instead of surfacing as silently-wrong ADR data later:

- An unknown canonical field name in the mapping (e.g. a typo like `scop`).
- A collision, where two canonical fields would resolve to the same YAML key -- either two mapped fields pointing at the same key, or a mapped field's key colliding with a still-default (unmapped) field's own canonical key.

An unset or empty `frontmatter_mappings` validates to `(nil, nil)`, preserving today's behavior byte-for-byte.

`index.ParseADRContent` takes the validated mapping as a new parameter and decodes frontmatter through a generic `map[string]yaml.Node` instead of directly into the `FrontMatter` struct whenever any mapping is configured (with no mappings, it decodes via `FrontMatter`'s own yaml tags exactly as before). Each canonical field is resolved to its source key -- the mapped key if remapped, its own canonical key otherwise -- and decoded from that key's node if present; a field with no matching key stays at its zero value, so `similarity_threshold`'s nil-vs-explicit-zero distinction (docs/arch/0011) is preserved for a remapped key exactly as it already is for the default key. `ParseADR` takes the same mapping and threads it through to `ParseADRContent`.

`LocalProvider` and `ConfluenceProvider` each gain a `SetFrontmatterMappings(map[string]string)` setter, mirroring the existing `SetIDPattern`/`SetWriter` plumbing pattern; `internal/cli.runCheck` and `runIndex` call it on each provider they construct, the same way they already call `SetIDPattern` on `LocalProvider`. Both providers funnel through the shared `ParseADRContent`, so the mapping applies identically regardless of ADR source.

## Consequences

- Corpora using non-canonical frontmatter field names can adopt ArchGuard by mapping just the fields that differ, without renaming any ADR files or replacing the whole frontmatter schema.
- Existing configs are unaffected: an unset `frontmatter_mappings` skips the generic-map decode path entirely and behaves exactly as before.
- A field left unmapped keeps reading its canonical key with zero behavior change, even when other fields in the same corpus are remapped.
- Misconfiguration (unknown field name, or a mapping that would make two fields collide on one YAML key) is caught once at startup, not per-ADR during indexing or checking.
- As with any change to what a field's value resolves to (see docs/arch/0008's discussion of hash-based rebuild triggers), enabling or changing `frontmatter_mappings` on an already-indexed corpus is not itself detected by `LocalStore.CalculateHash` (which hashes `RelPath`/`Content`/`ID`, not `Title`/`Status`/`Scope`/`SimilarityThreshold`) or by `PgStore`'s constant hash. A corpus adopting or changing `frontmatter_mappings` should run `archguard index` explicitly to pick up the newly-resolved field values.
