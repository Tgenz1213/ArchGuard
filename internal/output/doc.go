/*
Package output formats everything archguard prints for people: progress, the
end-of-run report, the baseline report, GitHub annotations and color.

It is the one place human-readable output is laid out, so other packages hand
it findings, not text. Some writes are primary, and failing them fails the run;
others are best-effort.

It does not decide what a run found or what exit code it gets; package
analysis builds the report and package cli sets the exit code.

Terms:

  - Printer: the writer every package prints through, so a run's output can be
    quieted, colored or kept off stdout as one.
  - Gaps: what a check could not cover, such as skipped files, failed checks
    and unindexed ADRs.
  - Coverage: how many files a run judged and ADR checks it made, and how many
    files had no ADR close enough to check against.
*/
package output
