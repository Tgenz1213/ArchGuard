package testutil

const (
	MockViolationTrigger = "password"

	MockEmbedFailureTrigger = "TRIGGER_EMBED_FAILURE"

	MockChatFailureTrigger = "TRIGGER_CHAT_FAILURE"

	// MockInterruptTrigger makes the mock providers cancel the run's context, as SIGINT would.
	MockInterruptTrigger = "TRIGGER_INTERRUPT"

	// MockChatProviderMarker and MockEmbedProviderMarker are shared with archguard-e2e's mocks so assertions can't drift from what they print.
	MockChatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	MockEmbedProviderMarker = "Using Mock Embed LLM Provider (E2E)"
)
