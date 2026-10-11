package testutil

const (
	MockViolationTrigger = "password"

	// MockRequiredMarker tags a line the mock treats as required by the ADR, so removing it is a violation.
	MockRequiredMarker = "archguard-required"

	MockEmbedFailureTrigger = "TRIGGER_EMBED_FAILURE"

	MockChatFailureTrigger = "TRIGGER_CHAT_FAILURE"

	// MockUnverifiedTrigger makes the mock chat provider report a violation quoting code that is not in the file.
	MockUnverifiedTrigger = "TRIGGER_UNVERIFIED_QUOTE"

	// MockInterruptTrigger makes the mock providers cancel the run's context, as SIGINT would.
	MockInterruptTrigger = "TRIGGER_INTERRUPT"

	// MockChatProviderMarker and MockEmbedProviderMarker are shared with archguard-e2e's mocks so assertions can't drift from what they print.
	MockChatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	MockEmbedProviderMarker = "Using Mock Embed LLM Provider (E2E)"
)
