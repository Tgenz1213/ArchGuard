package testutil

const (
	// MockViolationTrigger is the specific string that the mock LLM provider
	// looks for to simulate an architectural violation during E2E testing.
	MockViolationTrigger = "password"

	// MockChatProviderMarker and MockEmbedProviderMarker are printed by
	// cmd/archguard-e2e/main.go's mock providers when their Chat/CreateEmbedding
	// methods are actually invoked. Shared here so the e2e test assertions
	// checking for these strings can't silently drift out of sync with what
	// the mocks actually print.
	MockChatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	MockEmbedProviderMarker = "Using Mock Embed LLM Provider (E2E)"
)
