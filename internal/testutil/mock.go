package testutil

const (
	// MockViolationTrigger is the specific string that the mock LLM provider
	// looks for to simulate an architectural violation during E2E testing.
	MockViolationTrigger = "password"

	// MockChatProviderMarker and MockEmbedProviderMarker are shared with archguard-e2e's mocks so assertions can't drift from what they print.
	MockChatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	MockEmbedProviderMarker = "Using Mock Embed LLM Provider (E2E)"
)
