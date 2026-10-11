package inference

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInputTooLong = errors.New("input exceeds the embedding model's limit")

const maxShortenings = 12

// EmbedToFit returns the embedded text too, so a caller can tell whether it was shortened.
func EmbedToFit(ctx context.Context, embedder Embedder, text string, task EmbeddingTaskType) (Embedding, string, error) {
	for attempt := 0; ; attempt++ {
		embedding, err := embedder.CreateEmbedding(ctx, text, task)
		if !errors.Is(err, ErrInputTooLong) || attempt == maxShortenings {
			return embedding, text, err
		}

		shorter := shortenForEmbedding(text)
		if shorter == "" {
			return Embedding{}, text, err
		}

		text = shorter
	}
}

func shortenForEmbedding(text string) string {
	cut := len(text) * 3 / 4
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	if newline := strings.LastIndexByte(text[:cut], '\n'); newline > cut/2 {
		cut = newline
	}

	return text[:cut]
}
