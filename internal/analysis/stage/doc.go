/*
Package stage narrows a file's candidate ADRs before the model is asked about
them.

A Scorer gives each candidate a score, and a Stage keeps the candidates that
clear its threshold, best first, up to its limit. The configured stages run
one after another, rank, rerank, then pre_judge, and each sees only what the
one before it kept.

It never finds violations; package analysis asks the model about the
candidates that survive, and a screening stage only decides which of them it
asks about. Package config parses the pipeline settings, and package inference
talks to Jev.

Terms:

  - Cosine stage: scores each candidate by how similar its embedding is to the
    file's.
  - Jev stage: screens candidates by asking Jev whether the file breaks each
    of their rules, scoring an ADR by its likeliest-broken rule.
  - Pass-through: a candidate a scorer does not judge, such as an ADR with no
    rules in a Jev stage. The stage keeps it whatever its threshold or limit.
  - Skipped: a stage that failed under on_error: skip and passed its
    candidates on unchanged, rather than the file being skipped.
  - Unavailable: a stage failed while running, such as a provider error.
  - Precondition not met: a stage is missing something it needs to run, such
    as an embedder.
*/
package stage
