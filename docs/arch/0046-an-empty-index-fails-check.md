---
title: An empty index fails check with exit code 5
status: Accepted
scope: "internal/cli/**"
---

# An empty index fails check with exit code 5

## Context

`check` rebuilds a missing or stale local index (#174), but never builds a Postgres index: `PgStore.Load` only ensures the schema, and its hash is a constant. A Postgres table with no rows for the project, before the first `archguard index` or after a truncate, loaded without error. Every file then had no relevant ADR, and the run exited `0` with "No new architectural violations found." See #175.

## Decision

- **After the index loads, `check` counts the ADRs it holds for the project.** When there are none, the run fails with exit code `5` and tells the user to run `archguard index`. Nothing is analysed and nothing is rebuilt.
- **The count applies to both stores.** A local rebuild with no valid ADR already fails (`docs/arch/0010-index-corpus-health-reporting.md`), so this only catches a local index that loads with no ADRs, which no build writes.
- **Rows that belong to another `project_name` do not count.**

## Rules

- `archguard check` MUST NOT analyse files against an index that holds no ADRs for the project.
- `archguard check` MUST NOT build or rebuild a Postgres index.

## Alternatives considered

- **Rebuild the Postgres index as `check` does a local one.** `check` would write to a database that may be shared, from CI credentials that may only read it.
- **A corpus hash stored in Postgres.** It would also detect a stale index, which is more than this bug needs, and `check` still could not act on it without rebuilding.
- **A `--fail-on-empty-index` flag.** The default would keep passing silently.
- **Exit `8`.** That code means errors left the run with no ADR check; an empty index is an index problem and `5` already names it.

## Consequences

- A pipeline pointed at a new or emptied Postgres index fails until `archguard index` runs against it.
- An index that holds some but not all of the project's ADRs still runs, and the report lists the missing ones.
