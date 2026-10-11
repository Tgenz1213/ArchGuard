/*
Package cache keeps the model's answers on disk, so a repeated question is not
asked again.

A cached answer is keyed by everything that could change it: the model, the
ADR, the code and the prompts. Changing any of them, including a prompt's
wording, asks the model afresh.

It does not decide when to use a cached answer; package analysis does.

Terms:

  - Analysis key: identifies a question about whether a file breaks an ADR.
  - Suggestion key: identifies a request for how to fix a violation, kept apart
    so the two kinds of answer never collide.
*/
package cache
