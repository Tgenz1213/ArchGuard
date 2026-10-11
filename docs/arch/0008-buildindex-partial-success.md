---
title: "BuildIndex isolates per-ADR embedding failures"
status: "Accepted"
scope: "internal/**"
---

# BuildIndex isolates per-ADR embedding failures

## Context

Both vector store backends embedded new and modified ADRs concurrently, and the first error cancelled the whole build. One ADR the embedding provider rejected, such as one over the provider's input token limit, aborted indexing for every ADR. Because `check` rebuilds the index when the corpus hash changes, one bad ADR could block `archguard check` too, not just `archguard index`. In Postgres it was worse: sibling writes had often committed before the build stopped, leaving the database partially synced with no signal distinguishing that from a clean run.

## Decision

`BuildIndex` treats one ADR's embedding failure, and in Postgres its write failure, as non-fatal. A failing embed never cancels its siblings. The failure is recorded in the build result's skipped list with the ADR's path and error, and the ADR is left out of this run instead of partially applied. The local store drops it from the corpus; Postgres never writes it, so any earlier row stays as it was.

The local index hash covers every fetched ADR, not only the ones that embedded. The hash answers whether the saved index corresponds to the ADR files on disk, which a partial index still does. Which files embedded is the skipped list's job. Hashing only the embedded subset would make the saved hash disagree with the fresh one whenever an ADR keeps failing. Every `check` would then rebuild, skip the ADR again, fail to reload and exit with an index error: an endless rebuild loop. The accepted trade-off is that once a failing ADR stops changing, `check` stops rebuilding for it. A manual `archguard index` retries it, because a failed ADR is never saved and has no embedding to reuse. `check` lists such an ADR in its report as not in the index (`docs/arch/0028-check-report-and-log.md`).

`VectorStore.BuildIndex` returns a result alongside its error. The error is reserved for build-wide failures that make the run meaningless, such as fetching the corpus, reaching the database or hashing the corpus, never for one ADR's embedding failure. Two cases are deliberately build-wide rather than a pile of per-ADR skips:

- A cancelled context fails every in-flight and queued embed at once. It surfaces as one error, not as every ADR skipped plus a truncated index reported as success.
- If every ADR that needed embedding failed, the build returns an error and leaves the stored index untouched. An empty corpus would otherwise load cleanly and pass every file, which is worse than the abort this ADR replaces.

`archguard index` reports skipped ADRs and why as part of its corpus-health summary (`docs/arch/0010-index-corpus-health-reporting.md`), and still succeeds as long as the build completed and at least one ADR is valid.

## Consequences

- One oversized or rejected ADR does not take down indexing for the rest of the corpus, nor block `archguard check` through its automatic rebuild.
- Postgres cannot end up partially synced from one bad ADR. A failed ADR's row, new or existing, is left as it was; only successfully embedded ADRs are written.
- Callers that care only about hard failures check the error. Callers that want to see partial success must read the skipped list. Ignoring it would reintroduce a milder form of the original problem: claiming success while data is quietly missing.
- A total embedding outage stays loud: it fails the build rather than installing an empty index that reports every file clean. Partial success is tolerated; total failure is not.
- Postgres reindex-churn accounting (`vector_store.reindex_threshold`) counts only ADRs written in the run, not every ADR attempted. A run with many skips and few writes reads as low churn.
