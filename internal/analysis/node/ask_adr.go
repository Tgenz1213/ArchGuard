package node

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
)

const codeEntry = "code"

// ADRAsk scores each candidate by the probability of yes to one question about it, asked of each side of the file with every candidate ADR in the state.
type ADRAsk struct {
	Asker inference.Asker
	// Asked once per candidate, with {code} and {adr} naming the state's entries; empty asks the governs preset's question.
	Instructions string
}

// A file with no text is governed by no ADR, so every candidate scores 0 rather than passing on uncapped.
// A file too large to screen is an error, so it is skipped rather than kept or dropped unscreened.
func (a *ADRAsk) Score(ctx context.Context, file File, _ Debug, candidates []Candidate) ([]float64, error) {
	sides, err := nonBlankSides(file)
	if err != nil {
		return nil, err
	}

	scores := make([]float64, len(candidates))
	if len(sides) == 0 {
		return scores, nil
	}

	entries := make([]string, len(candidates))
	questions := make(map[string]string, len(candidates))
	state := make(map[string]string, len(candidates)+1)
	code := strings.ReplaceAll(a.instructions(), "{code}", "`"+codeEntry+"`")

	for i, candidate := range candidates {
		entries[i] = fmt.Sprintf("adr%d", i)
		state[entries[i]] = candidate.ADR.Content
		questions[entries[i]] = strings.ReplaceAll(code, "{adr}", "`"+entries[i]+"`")
	}

	answers, err := askEachSide(ctx, sides, questions, "candidate ADRs", func(ctx context.Context, side string) (map[string]float64, error) {
		asked := maps.Clone(state)
		asked[codeEntry] = side

		return a.Asker.AskAbout(ctx, asked, questions)
	})
	if err != nil {
		return nil, err
	}

	for i, entry := range entries {
		scores[i] = answers[entry]
	}

	return scores, nil
}

func (a *ADRAsk) instructions() string {
	if a.Instructions == "" {
		return config.GovernsInstructions
	}

	return a.Instructions
}

func NewADRAskNode(asker inference.Asker, instructions string, threshold float64, topK int) Node {
	return Node{
		Name:                  "ask",
		Scorer:                &ADRAsk{Asker: asker, Instructions: instructions},
		Min:                   FixedMin(threshold),
		MaxKeep:               topK,
		PassThroughOnSkip:     true,
		Screens:               true,
		SkipsUnscreenableFile: true,
	}
}
