/*
Package cli is archguard's command line: init, index and check.

It parses the command line before touching git or the configuration, so help
and --version work outside a repository. It then works from the repository
root, so every path is relative to it, builds the providers and the ADR store
from archguard.yaml and the environment, and runs the command. It owns the
exit codes and decides when a saved index is stale enough to rebuild before a
check.

It does not judge code or format output. Package analysis runs the check,
package output renders what it found, and package telemetry records what the
run did.

Terms:

  - Gap: something a check could not cover, such as a skipped file, a failed
    check or an ADR missing from the index. Gaps are reported next to the
    violations, so a clean check can be told apart from one that covered less
    than it was asked to.
  - Provider factories: what a caller of Execute passes to replace the real
    providers or add a recorder, as the end-to-end tests do. Production
    passes none.
*/
package cli
