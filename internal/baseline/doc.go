/*
Package baseline reads and writes archguard-baseline.json, the committed list
of violations a team has accepted for now.

A baselined violation is hidden from later checks for as long as the code it
quoted is still in the file. While it stays, any violation of that ADR in that
file is hidden; when it goes, so does the suppression.

It does not decide what counts as a violation or when the file is rewritten:
package analysis matches findings against it, and package cli updates it for
check --update-baseline.

Terms:

  - Entry: one accepted violation, keyed by its ADR and file, with the code it
    quoted and an optional reason saying why it was accepted.
*/
package baseline
