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

// QuestionSource writes screening questions for an ADR that has no rules, each phrased so yes means a violation.
type QuestionSource interface {
	Questions(ctx context.Context, adr *index.ADR) ([]string, error)
}

// JevScreen scores a candidate by its likeliest-broken rule. A candidate with no rules passes through,
// unless Generated is set and writes questions for it.
type JevScreen struct {
	Screener  Screener
	Generated QuestionSource
}

type ruleQuestion struct {
	candidate int
	text      string
}

func (j *JevScreen) Score(ctx context.Context, file File, debug Debug, candidates []Candidate) ([]float64, error) {
	scores := make([]float64, len(candidates))
	for i := range scores {
		scores[i] = math.Inf(1)
	}

	state := file.JudgedText()
	if strings.TrimSpace(state) == "" {
		return scores, nil
	}

	questions, err := j.questions(ctx, debug, candidates)
	if err != nil {
		return nil, err
	}

	if len(questions) == 0 {
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

func (j *JevScreen) questions(ctx context.Context, debug Debug, candidates []Candidate) (map[string]ruleQuestion, error) {
	questions := make(map[string]ruleQuestion)
	for i, candidate := range candidates {
		for k, rule := range candidate.ADR.Rules {
			questions[fmt.Sprintf("adr%d_rule%d", i, k)] = ruleQuestion{candidate: i, text: questionText(rule)}
		}

		if len(candidate.ADR.Rules) > 0 || j.Generated == nil {
			continue
		}

		generated, err := j.Generated.Questions(ctx, candidate.ADR)
		if err != nil {
			return nil, &Error{Action: "generating questions", Err: err}
		}

		for k, text := range generated {
			debug.Debug("Generated question for %s: %s", candidate.ADR.Title, text)
			questions[fmt.Sprintf("adr%d_generated%d", i, k)] = ruleQuestion{candidate: i, text: text}
		}
	}

	return questions, nil
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

// A nil generated leaves ADRs without rules unscreened.
func NewJevStage(screener Screener, generated QuestionSource, threshold float64, topK int) Stage {
	return Stage{
		Name:              "pre_judge",
		Scorer:            &JevScreen{Screener: screener, Generated: generated},
		Min:               FixedMin(threshold),
		MaxKeep:           topK,
		PassThroughOnSkip: true,
	}
}
