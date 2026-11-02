/*
Package node narrows a file's candidate ADRs before the model is asked about
them.

A Scorer gives each candidate a score, and a Node keeps the candidates that
clear its threshold, best first, up to its limit. Each rank and ask node in
the pipeline is one Node with that node's name; they run in pipeline order,
and each sees only what the one before it kept.

It never finds violations; package analysis asks the model about the
candidates that survive, and a screening node only decides which of them it
asks about. Package config parses the pipeline settings, and package inference
talks to Jev.

Terms:

  - Rank node: run by cosine. It scores each candidate by how similar its
    embedding is to the file's.
  - Ask node: run by Jev. It screens candidates by asking Jev a question
    about each of their rules, scoring an ADR by its likeliest-broken rule.
  - Sides: what a scorer that answers without a quote reads, the whole file or
    each side of a diff, so a rule broken by removing code is still seen.
  - Pass-through: a candidate a scorer does not judge, such as an ADR with no
    rules in an ask node. The node keeps it whatever its threshold or limit.
  - Skipped: a node that failed under on_error: skip and passed its
    candidates on unchanged, rather than the file being skipped.
  - Unavailable: a node failed while running, such as a provider error.
  - Precondition not met: a node is missing something it needs to run, such
    as an embedder.
*/
package node
