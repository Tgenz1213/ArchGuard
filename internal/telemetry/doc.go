/*
Package telemetry records what a run did: model requests, cache checks,
retries and files checked.

Every event goes through one Recorder, so a new kind of report reads the same
events instead of adding counters across the code. Meters wrap the chat and
embedding providers so their calls are recorded without the callers knowing.

It does not price usage or print anything; package cli builds the report's
usage section from the totals, and package output renders it.

Terms:

  - Role: what a model call was for: judging, suggesting a fix, or embedding.
  - Tally: the running totals the report's usage section is built from.
*/
package telemetry
