//go:build probe

package inference

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ollama/ollama/api"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

func TestProbeOversizedEmbedding(t *testing.T) {
	text := strings.Repeat("word ", 50000)
	cases := []struct {
		name string
		key  string
		e    func(key string) Embedder
	}{
		{"openai text-embedding-3-small", "OPENAI_API_KEY", func(k string) Embedder { return NewOpenAIProvider(k, "", "text-embedding-3-small") }},
		//{"gemini gemini-embedding-2", "GEMINI_API_KEY", func(k string) Embedder { return NewGeminiProvider(k, "", "gemini-embedding-2") }},
		//{"gemini gemini-embedding-001", "GEMINI_API_KEY", func(k string) Embedder { return NewGeminiProvider(k, "", "gemini-embedding-001") }},
		//{"voyage voyage-4", "VOYAGE_API_KEY", func(k string) Embedder { return NewVoyageProvider(k, "voyage-4") }},
		//{"voyage voyage-context-3", "VOYAGE_API_KEY", func(k string) Embedder { return NewVoyageProvider(k, "voyage-context-3") }},
	}

	for _, tc := range cases {
		key := os.Getenv(tc.key)
		if key == "" {
			fmt.Printf("PROBE %s: key missing\n", tc.name)
			continue
		}

		emb, err := tc.e(key).CreateEmbedding(t.Context(), text, EmbeddingTaskQuery)
		var oerr *openai.Error
		var gerr genai.APIError
		var serr api.StatusError
		fmt.Printf("PROBE %s: len=%d err=%v openai=%v genai=%v ollama=%v\n", tc.name, len(emb), err, errors.As(err, &oerr), errors.As(err, &gerr), errors.As(err, &serr))
		if oerr != nil {
			fmt.Printf("PROBE %s: openai message=%q\n", tc.name, oerr.Message)
		}
		if errors.As(err, &gerr) {
			fmt.Printf("PROBE %s: genai code=%d status=%q\n", tc.name, gerr.Code, gerr.Status)
		}
	}
}
