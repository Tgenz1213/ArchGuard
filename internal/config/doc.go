/*
Package config loads and validates archguard.yaml.

It fills in defaults and rejects a malformed value with an error naming the
key. LoadConfig keeps analysis.pipeline and the jev block raw, and
ParsePipeline validates them, so only check rejects a bad pipeline. Settings
whose zero value means something are pointers, so an unset setting can take
its default.

It does not act on any setting. Checks that need more than one setting, such as
whether the chat and embedding providers pair up, are in package cli, which
also builds providers and stores from the configuration.

Terms:

  - Pipeline: the nodes, in order, that choose which ADRs reach the model for
    each file and then judge them.
  - Node: one step of the pipeline, with a name, a job (rank, ask or judge)
    and the runner that does it.
  - Preset: a named question and keep threshold an ask node can use instead
    of writing its own.
*/
package config
