/*
Package stage narrows a file's candidate ADRs before the model is asked about
them.

A Scorer gives each candidate a score, and a Stage keeps the candidates that
clear its threshold, best first, up to its limit. The configured stages run
one after another, rank then rerank, and each sees only what the one before it
kept.

It never finds violations; package analysis asks the model about the
candidates that survive. Package config parses the pipeline settings.

Terms:

  - Cosine stage: scores each candidate by how similar its embedding is to the
    file's.
  - Unavailable: a stage failed while running, such as a provider error.
  - Precondition not met: a stage is missing something it needs to run, such
    as an embedder.
*/
package stage
