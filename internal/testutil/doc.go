/*
Package testutil holds the markers and fixed figures shared by the end-to-end
tests and the mock providers in cmd/archguard-e2e, so the two cannot drift
apart.

A marker in a test file makes a mock provider behave a certain way, such as
reporting a violation or failing a call, and a fixed figure is what the mocks
report so tests can assert on it.
*/
package testutil
