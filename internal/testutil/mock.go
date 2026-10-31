package testutil

const (
	MockViolationTrigger = "password"

	// MockRequiredMarker tags a line the mock treats as required by the ADR, so removing it is a violation.
	MockRequiredMarker = "archguard-required"

	MockEmbedFailureTrigger = "TRIGGER_EMBED_FAILURE"

	MockChatFailureTrigger = "TRIGGER_CHAT_FAILURE"

	// MockUnverifiedTrigger makes the mock chat provider report a violation quoting code that is not in the file.
	MockUnverifiedTrigger = "TRIGGER_UNVERIFIED_QUOTE"

	// archguard-e2e's mock Jev answers yes to a rule naming MockJevBreaksTrigger and fails on content holding MockJevFailureTrigger.
	MockJevBreaksTrigger  = "TRIGGER_JEV_BREAKS"
	MockJevFailureTrigger = "TRIGGER_JEV_FAILURE"
	// MockJevRejectedKeyTrigger makes the mock Jev answer as Jev does to a rejected API key (401).
	MockJevRejectedKeyTrigger = "TRIGGER_JEV_REJECTED_KEY"

	// MockInterruptTrigger makes the mock providers cancel the run's context, as SIGINT would.
	MockInterruptTrigger = "TRIGGER_INTERRUPT"

	// MockChatProviderMarker and MockEmbedProviderMarker are shared with archguard-e2e's mocks so assertions can't drift from what they print.
	MockChatProviderMarker  = "Using Mock Chat LLM Provider (E2E)"
	MockEmbedProviderMarker = "Using Mock Embed LLM Provider (E2E)"

	// MockEmbedDocumentMarker prints once per ADR embedding, so tests can count them apart from query embeddings.
	MockEmbedDocumentMarker = "Mock embedded an ADR (E2E)"

	// What archguard-e2e's mocks report for every call, so usage figures can be asserted.
	MockChatInputTokens  = 100
	MockChatOutputTokens = 10
	MockEmbedTokens      = 5
)
