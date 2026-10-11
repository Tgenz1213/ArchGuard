---
title: Index corpus health reporting
status: Accepted
scope: "internal/**"
---

# Index corpus health reporting

## Context

`archguard index` printed a count of valid ADRs and a generic success message, however many ADRs were excluded along the way. Parse failures printed one warning each with no total, and ADRs rejected for their status printed nothing. Duplicate ADR IDs went undetected, which silently breaks `archguard-ignore` and baseline suppression, since both key on the ADR ID. See #136.

## Decision

Each `index.Provider` reports what it found beyond the valid ADRs: how many ADRs it discovered, which failed to parse, how many it rejected for their status, and which accepted ADRs loaded without their rules (docs/arch/0023). A composite provider sums these across its providers.

Duplicate IDs and ADRs with no scope are detected once, after the providers' results are merged. A collision can span providers, such as a local file and a Confluence page sharing an ID, so no single provider can see it.

An ADR counts as valid only once it is indexed, not merely when its status is accepted.

`archguard index` prints a corpus summary whenever the ADR fetch was attempted, including when the build then fails. The build result records whether the fetch was attempted, because "the fetch never happened" and "the fetch found nothing" look the same from the counts alone.

The run fails with the index error exit code when no valid ADR survives, whatever was discovered. Skipped, rejected and duplicate ADRs are reported but do not fail the run. A corpus with nothing discovered, such as a misconfigured `adr_path`, is exactly the case where a rebuild would otherwise empty a healthy Postgres index while reporting success. The emptiness check runs before the local index is saved, so a failed or empty rebuild leaves the index on disk untouched.

## Rules

- Each `index.Provider` MUST report how many ADRs it discovered, which failed to parse, how many it rejected for their status, and which accepted ADRs loaded without their rules.
- Duplicate IDs and ADRs with no scope MUST be detected once, after the providers' results are merged.
- `archguard index` MUST fail with the index error exit code when no valid ADR survives.
- Skipped, rejected and duplicate ADRs MUST be reported and MUST NOT fail the run.
- The emptiness check MUST run before the local index is saved.

## Alternatives considered

None recorded.

## Consequences

- After every `archguard index`, a user sees what happened to every discovered ADR: valid, failed to parse, rejected for its status, failed to embed, or in a duplicate-ID collision.
- `archguard index`, and `archguard check --ci` when it rebuilds the index, exit non-zero for a corpus with no valid ADRs, including an empty directory. This is deliberate. `archguard init` scaffolds `ADR_TEMPLATE.md` with a placeholder status, so running `index` before writing a real ADR fails loudly instead of leaving a `check` that always passes.
- Every `Provider` implementation must report these counts. The interface has few implementers, so the cost is small.
