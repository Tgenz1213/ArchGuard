/*
Package analysis is the engine behind archguard check. For each file it decides
which ADRs to ask about, asks the model whether the file breaks them, and
collects what the run found into a report.

The model is the only source of violations. The scoring stages in package
stage only choose which ADRs reach it, and a finding counts only when its quote
is found in the text the model read.

It does not choose which files are checked: a ContentProvider supplies them,
and package cli picks one. It does not print anything or set the exit code;
package cli does that, and package output renders the report. ADRs come from
package index, models from package inference, recorded findings from package
baseline, and earlier answers from package cache.

Terms:

  - Candidate: an ADR whose scope covers the file, before the scoring stages
    decide whether the model sees it.
  - Suppression: an archguard-ignore comment in a file, which removes an ADR
    from that file's candidates.
  - Unverified finding: a violation whose quote is not in what the model read.
    It is reported apart and never fails the run.
  - Warn-open: under --ci, a file too large to send whole is skipped with a
    warning instead of being cut short.
*/
package analysis
