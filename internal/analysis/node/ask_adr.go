package node

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/tgenz1213/archguard/internal/inference"
)

const codeEntry = "code"

// ADRAsk scores each candidate by the probability of yes to one question about it, asked of each side of the file with every candidate ADR in the state.
type ADRAsk struct {
	Asker inference.Asker
	// Asked once per candidate, with {code} and {adr} naming the state's entries.
	Instructions string
}

func (a *ADRAsk) Score(ctx context.Context, file File, _ Debug, candidates []Candidate) ([]float64, error) {
	sides := slices.DeleteFunc(slices.Clone(file.Sides()), func(side string) bool { return strings.TrimSpace(side) == "" })
	if len(sides) == 0 {
		return unscoredByThisNode(len(candidates)), nil
	}

	questions := make(map[string]string, len(candidates))
	state := make(map[string]string, len(candidates)+1)

	for i, candidate := range candidates {
		entry := adrEntry(i)
		state[entry] = candidate.ADR.Content
		questions[entry] = strings.NewReplacer("{code}", "`"+codeEntry+"`", "{adr}", "`"+entry+"`").Replace(a.Instructions)
	}

	scores := make([]float64, len(candidates))

	for n, side := range sides {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		state[codeEntry] = side

		answers, err := a.Asker.AskAbout(ctx, state, questions)
		if err != nil {
			return nil, askError(err, "candidate ADRs")
		}

		for i := range candidates {
			probability, ok := answers[adrEntry(i)]
			if !ok {
				return nil, &Error{Action: askAction, Err: fmt.Errorf("no answer for question %s", adrEntry(i))}
			}

			if n == 0 || probability > scores[i] {
				scores[i] = probability
			}
		}
	}

	return scores, nil
}

func adrEntry(i int) string { return fmt.Sprintf("adr%d", i) }

func NewADRAskNode(asker inference.Asker, instructions string, threshold float64, topK int) Node {
	return Node{
		Name:              "ask",
		Scorer:            &ADRAsk{Asker: asker, Instructions: instructions},
		Min:               FixedMin(threshold),
		MaxKeep:           topK,
		PassThroughOnSkip: true,
	}
}
