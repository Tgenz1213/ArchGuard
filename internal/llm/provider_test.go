package llm

import "testing"

func TestProviderRoles(t *testing.T) {
	tests := []struct {
		name      string
		provider  any
		wantChat  bool
		wantEmbed bool
	}{
		{"openai", NewOpenAIProvider("key", "chat-model", "embed-model"), true, true},
		{"ollama", NewOllamaProvider("http://localhost:11434", "chat-model", "embed-model", 0), true, true},
		{"gemini", NewGeminiProvider("key", "chat-model", "embed-model"), true, true},
		{"claude", NewClaudeProvider("key", "chat-model"), true, false},
		{"voyage", NewVoyageProvider("key", "embed-model"), false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := tt.provider.(Chatter); ok != tt.wantChat {
				t.Errorf("implements Chatter = %v, want %v", ok, tt.wantChat)
			}

			if _, ok := tt.provider.(Embedder); ok != tt.wantEmbed {
				t.Errorf("implements Embedder = %v, want %v", ok, tt.wantEmbed)
			}
		})
	}
}
