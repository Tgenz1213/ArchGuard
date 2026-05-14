package testutil

const (
	// MockViolationTrigger is the specific string that the mock LLM provider
	// looks for to simulate an architectural violation during E2E testing.
	MockViolationTrigger = "password"
)
