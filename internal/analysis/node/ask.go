package node

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

const askAction = "asking about candidates"

type inputTooLargeError struct {
	err error
	// What else was sent beside the file, since the model's error doesn't say which was too large.
	alongside string
}

func (e *inputTooLargeError) Error() string {
	return "the file or its " + e.alongside + " are over the model's input limit"
}

func (e *inputTooLargeError) Unwrap() error { return e.err }

// RuleAsk scores a candidate by its likeliest-broken rule; a candidate with no rules passes through.
type RuleAsk struct {
	Asker inference.Asker
	// Asked once per rule with {rule} replaced; empty asks the breaks-a-rule preset's question.
	Instructions string
}

func (r *RuleAsk) Score(ctx context.Context, file File, _ Debug, candidates []Candidate) ([]float64, error) {
	scores := unscoredByThisNode(len(candidates))

	asked, owner := ruleQuestions(r.instructions(), candidates)
	if len(asked) == 0 {
		return scores, nil
	}

	sides := nonBlankSides(file)
	if len(sides) == 0 {
		return scores, nil
	}

	answers, err := askEachSide(ctx, sides, asked, "rules", func(ctx context.Context, side string) (map[string]float64, error) {
		return r.Asker.Ask(ctx, side, asked)
	})
	if err != nil {
		return nil, err
	}

	for name, candidate := range owner {
		probability := answers[name]
		if math.IsInf(scores[candidate], 1) || probability > scores[candidate] {
			scores[candidate] = probability
		}
	}

	return scores, nil
}

func nonBlankSides(file File) []string {
	return slices.DeleteFunc(slices.Clone(file.Sides()), func(side string) bool { return strings.TrimSpace(side) == "" })
}

// askEachSide asks every question about each side and keeps each question's higher answer, so what a change removes still counts.
// Every side must answer every question; one side never fills in for the other.
func askEachSide(ctx context.Context, sides []string, questions map[string]string, alongside string, ask func(ctx context.Context, side string) (map[string]float64, error)) (map[string]float64, error) {
	highest := make(map[string]float64, len(questions))

	for _, side := range sides {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		answers, err := ask(ctx, side)
		if err != nil {
			return nil, askError(err, alongside)
		}

		for name := range questions {
			probability, ok := answers[name]
			if !ok {
				return nil, &Error{Action: askAction, Err: fmt.Errorf("no answer for question %s", name)}
			}

			if previous, seen := highest[name]; !seen || probability > previous {
				highest[name] = probability
			}
		}
	}

	return highest, nil
}

func (r *RuleAsk) instructions() string {
	if r.Instructions == "" {
		return config.BreaksARuleInstructions
	}

	return r.Instructions
}

// ruleQuestions returns each question's text and the index of the candidate it asks about, both by question name.
func ruleQuestions(instructions string, candidates []Candidate) (map[string]string, map[string]int) {
	texts := make(map[string]string)
	owner := make(map[string]int)

	for i, candidate := range candidates {
		for j, rule := range candidate.ADR.Rules {
			name := fmt.Sprintf("adr%d_rule%d", i, j)
			texts[name] = strings.ReplaceAll(instructions, "{rule}", ruleText(rule))
			owner[name] = i
		}
	}

	return texts, owner
}

func unscoredByThisNode(n int) []float64 {
	scores := make([]float64, n)
	for i := range scores {
		scores[i] = math.Inf(1)
	}

	return scores
}

func ruleText(rule index.Rule) string {
	var text strings.Builder

	text.WriteString(rule.Statement)

	if len(rule.Violating) > 0 {
		text.WriteString("\nCode like this breaks it:\n" + strings.Join(rule.Violating, "\n"))
	}

	if len(rule.Compliant) > 0 {
		text.WriteString("\nCode like this follows it:\n" + strings.Join(rule.Compliant, "\n"))
	}

	return text.String()
}

func askError(err error, alongside string) error {
	if errors.Is(err, inference.ErrInputTooLarge) {
		return &Error{Action: askAction, Kind: KindPreconditionNotMet, Err: &inputTooLargeError{err: err, alongside: alongside}}
	}

	if errors.Is(err, inference.ErrKeyRejected) {
		return &Error{Action: askAction, Err: err, KeyRejected: true}
	}

	return &Error{Action: askAction, Err: err}
}

func NewAskNode(asker inference.Asker, instructions string, threshold float64, topK int) Node {
	return Node{
		Name:              "ask",
		Scorer:            &RuleAsk{Asker: asker, Instructions: instructions},
		Min:               FixedMin(threshold),
		MaxKeep:           topK,
		PassThroughOnSkip: true,
		Screens:           true,
	}
}
