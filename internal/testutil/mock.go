package testutil

const (
	// MockViolationTrigger is the specific string that the mock LLM provider
	// looks for to simulate an architectural violation during E2E testing.
	MockViolationTrigger = "password"

	// MockEmbedFailureTrigger is the specific string that the mock LLM
	// providers look for to simulate an embedding failure during E2E testing.
	MockEmbedFailureTrigger = "TRIGGER_EMBED_FAILURE"

	// MockChatProviderMarker and MockEmbedProviderMarker are shared with archguard-e2e's mocks so assertions can't drift from what they print.
	MockChatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	MockEmbedProviderMarker = "Using Mock Embed LLM Provider (E2E)"
)
