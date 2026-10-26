package stage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

const screeningAction = "screening candidates"

// Screener asks yes/no questions about one text in a single call and returns each answer's probability of yes, by question name.
type Screener interface {
	Screen(ctx context.Context, state string, questions map[string]string) (map[string]float64, error)
}

// JevScreen scores a candidate by its likeliest-broken rule; a candidate with no rules passes through.
type JevScreen struct {
	Screener Screener
}

type ruleQuestion struct {
	candidate int
	text      string
}

func (j *JevScreen) Score(ctx context.Context, file File, _ Debug, candidates []Candidate) ([]float64, error) {
	scores := make([]float64, len(candidates))
	for i := range scores {
		scores[i] = math.Inf(1)
	}

	questions := ruleQuestions(candidates)
	state := file.JudgedText()

	if len(questions) == 0 || strings.TrimSpace(state) == "" {
		return scores, nil
	}

	asked := make(map[string]string, len(questions))
	for name, question := range questions {
		asked[name] = question.text
	}

	answers, err := j.Screener.Screen(ctx, state, asked)
	if err != nil {
		return nil, screeningError(err)
	}

	for name, question := range questions {
		probability, ok := answers[name]
		if !ok {
			return nil, &Error{Action: screeningAction, Err: fmt.Errorf("no answer for question %s", name)}
		}

		if math.IsInf(scores[question.candidate], 1) || probability > scores[question.candidate] {
			scores[question.candidate] = probability
		}
	}

	return scores, nil
}

func ruleQuestions(candidates []Candidate) map[string]ruleQuestion {
	questions := make(map[string]ruleQuestion)
	for i, candidate := range candidates {
		for j, rule := range candidate.ADR.Rules {
			questions[fmt.Sprintf("adr%d_rule%d", i, j)] = ruleQuestion{candidate: i, text: questionText(rule)}
		}
	}

	return questions
}

func questionText(rule index.Rule) string {
	var text strings.Builder

	text.WriteString("Does this code break the following rule? " + rule.Statement)

	if len(rule.Violating) > 0 {
		text.WriteString("\nCode like this breaks it:\n" + strings.Join(rule.Violating, "\n"))
	}

	if len(rule.Compliant) > 0 {
		text.WriteString("\nCode like this follows it:\n" + strings.Join(rule.Compliant, "\n"))
	}

	return text.String()
}

func screeningError(err error) error {
	var jevErr *inference.JevError
	if errors.As(err, &jevErr) && jevErr.TooLarge() {
		return &Error{Action: screeningAction, Kind: KindPreconditionNotMet, Err: err}
	}

	return &Error{Action: screeningAction, Err: err}
}

func NewJevStage(screener Screener, threshold float64, topK int) Stage {
	return Stage{
		Name:              "pre_judge",
		Scorer:            &JevScreen{Screener: screener},
		Min:               FixedMin(threshold),
		MaxKeep:           topK,
		PassThroughOnSkip: true,
	}
}
