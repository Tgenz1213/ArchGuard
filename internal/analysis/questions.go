package analysis

import (
	"context"
	"sync"

	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"golang.org/x/sync/singleflight"
)

// QuestionGenerator writes Jev screening questions for ADRs without rules, once per ADR across concurrent files.
type QuestionGenerator struct {
	chat  inference.Chatter
	cache *cache.Cache
	model string
	out   *output.Printer
	calls singleflight.Group
	mu    sync.Mutex
	known map[string][]string
}

// A nil cache regenerates questions on every run.
func NewQuestionGenerator(chat inference.Chatter, questionCache *cache.Cache, model string, out *output.Printer) *QuestionGenerator {
	return &QuestionGenerator{chat: chat, cache: questionCache, model: model, out: out, known: make(map[string][]string)}
}

func (g *QuestionGenerator) Questions(ctx context.Context, adr *index.ADR) ([]string, error) {
	key := cache.ComputeQuestionsKey(cache.QuestionsKeyInput{
		ModelName:      g.model,
		ADRContent:     adr.Content,
		SystemPrompt:   inference.QuestionSystemPrompt,
		PromptTemplate: inference.QuestionPrompt,
	})

	if questions, ok := g.lookup(key); ok {
		return questions, nil
	}

	// A caller that missed known just as another call finished finds the questions inside Do.
	loaded, err, _ := g.calls.Do(key, func() (any, error) {
		if questions, ok := g.lookup(key); ok {
			return questions, nil
		}

		questions, err := g.load(ctx, key, adr)
		if err != nil {
			return nil, err
		}

		g.mu.Lock()
		g.known[key] = questions
		g.mu.Unlock()

		return questions, nil
	})
	if err != nil {
		return nil, err
	}

	return loaded.([]string), nil
}

func (g *QuestionGenerator) lookup(key string) ([]string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	questions, ok := g.known[key]

	return questions, ok
}

func (g *QuestionGenerator) load(ctx context.Context, key string, adr *index.ADR) ([]string, error) {
	if g.cache != nil {
		cached, found, err := g.cache.GetQuestions(key)
		if err != nil {
			g.out.Warn("reading cached questions for ADR %s: %v", adr.ID, err)
		}

		if found {
			return cached, nil
		}
	}

	questions, _, err := inference.GenerateQuestions(ctx, g.chat, adr.Content)
	if err != nil {
		return nil, err
	}

	if g.cache != nil {
		if err := g.cache.PutQuestions(key, questions); err != nil {
			g.out.Warn("caching questions for ADR %s: %v", adr.ID, err)
		}
	}

	return questions, nil
}
