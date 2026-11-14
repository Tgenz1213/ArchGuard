package node

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
)

// ADRAsk scores each candidate by the probability of yes to one question about it, asked of each side of the file.
type ADRAsk struct {
	Asker inference.Asker
	// Asked once per candidate with {adr} replaced by its text; empty asks the governs preset's question.
	Instructions string
}

// A file with no text is governed by no ADR, so every candidate scores below any threshold, 0 included, rather than passing on uncapped.
func (a *ADRAsk) Score(ctx context.Context, file File, _ Debug, candidates []Candidate) ([]float64, error) {
	scores := make([]float64, len(candidates))

	sides := nonBlankSides(file)
	if len(sides) == 0 {
		for i := range scores {
			scores[i] = math.Inf(-1)
		}

		return scores, nil
	}

	questions := make(map[string]string, len(candidates))
	for i, candidate := range candidates {
		questions[adrQuestion(i)] = strings.ReplaceAll(a.instructions(), "{adr}", candidate.ADR.Content)
	}

	answers, err := askEachSide(ctx, a.Asker, sides, questions, "candidate ADRs")
	if err != nil {
		return nil, err
	}

	for i := range candidates {
		scores[i] = answers[adrQuestion(i)]
	}

	return scores, nil
}

func adrQuestion(i int) string { return fmt.Sprintf("adr%d", i) }

func (a *ADRAsk) instructions() string {
	if a.Instructions == "" {
		return config.GovernsInstructions
	}

	return a.Instructions
}

func NewADRAskNode(asker inference.Asker, instructions string, threshold float64, topK int) Node {
	return Node{
		Name:              "ask",
		Scorer:            &ADRAsk{Asker: asker, Instructions: instructions},
		Min:               FixedMin(threshold),
		MaxKeep:           topK,
		PassThroughOnSkip: true,
		Screens:           true,
	}
}
