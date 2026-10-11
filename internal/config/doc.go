/*
Package config loads and validates archguard.yaml.

It fills in defaults and rejects a malformed value, such as an unknown pipeline
stage, with an error naming the key. Settings whose zero value means something
are pointers, so an unset setting can take its default.

It does not act on any setting. Checks that need more than one setting, such as
whether the chat and embedding providers pair up, are in package cli, which
also builds providers and stores from the configuration.

Terms:

  - Pipeline: the scoring stages, in order, that choose which ADRs reach the
    model for each file.
  - Stage settings: a stage's threshold, how many candidates it keeps, and
    whether a failure skips the stage or fails the run.
*/
package config
