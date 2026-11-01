package node_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type fakeScreener struct {
	answer func(instructions string) float64
	err    error
	calls  []map[string]string
	states []string
}

func (f *fakeScreener) Screen(_ context.Context, state string, questions map[string]string) (map[string]float64, error) {
	f.calls = append(f.calls, questions)
	f.states = append(f.states, state)

	if f.err != nil {
		return nil, f.err
	}

	answers := make(map[string]float64, len(questions))
	for name, instructions := range questions {
		answers[name] = f.answer(instructions)
	}

	return answers, nil
}

// Answers 0.9 to a question naming any of the given rule statements, 0.1 to the rest.
func breaks(statements ...string) func(string) float64 {
	return func(instructions string) float64 {
		for _, statement := range statements {
			if strings.Contains(instructions, statement) {
				return 0.9
			}
		}

		return 0.1
	}
}

func ruledCandidate(id string, statements ...string) node.Candidate {
	rules := make(index.Rules, len(statements))
	for i, statement := range statements {
		rules[i] = index.Rule{Statement: statement}
	}

	return node.Candidate{ADR: &index.ADR{ID: id, Title: "ADR " + id, Rules: rules}, Score: 0.7}
}

func TestJevNode_KeepsADRsWithABrokenRuleInOneCall(t *testing.T) {
	screener := &fakeScreener{answer: breaks("no prints")}
	s := node.NewJevNode(screener, "", 0.3, 0)
	candidates := []node.Candidate{
		ruledCandidate("a", "no globals", "no prints"),
		ruledCandidate("b", "no globals"),
		ruledCandidate("c"),
	}

	got, err := s.Apply(t.Context(), fakeFile{text: "+fmt.Println(x)"}, node.NoDebug, candidates)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "a,c" {
		t.Errorf("kept %s, want a (a rule scored 0.9) and c (no rules, passed through)", ids(got))
	}

	if len(screener.calls) != 1 || len(screener.calls[0]) != 3 {
		t.Fatalf("calls = %v, want one call with a question per rule of a and b", screener.calls)
	}

	if screener.states[0] != "+fmt.Println(x)" {
		t.Errorf("state = %q, want the judged text with its diff markers", screener.states[0])
	}

	if got[0].Score != 0.9 || got[1].Score != 0.7 {
		t.Errorf("scores = %v, %v; want a's best rule score and c's earlier score", got[0].Score, got[1].Score)
	}
}

func TestJevNode_MakesNoCallWithoutQuestionsOrContent(t *testing.T) {
	tests := []struct {
		name       string
		candidates []node.Candidate
		text       string
	}{
		{"no candidate has rules", []node.Candidate{ruledCandidate("a"), ruledCandidate("b")}, "code"},
		{"empty content", []node.Candidate{ruledCandidate("a", "no prints")}, "  \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			screener := &fakeScreener{answer: breaks()}

			got, err := node.NewJevNode(screener, "", 0.3, 0).Apply(t.Context(), fakeFile{text: tt.text}, node.NoDebug, tt.candidates)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if len(got) != len(tt.candidates) || len(screener.calls) != 0 {
				t.Errorf("kept %s after %d call(s), want every candidate and no call", ids(got), len(screener.calls))
			}
		})
	}
}

func TestJevNode_QuestionCarriesTheRuleAndItsExamples(t *testing.T) {
	screener := &fakeScreener{answer: breaks()}
	candidate := node.Candidate{ADR: &index.ADR{ID: "a", Rules: index.Rules{{
		Statement: "Code MUST NOT print to stdout.",
		Violating: []string{"fmt.Println(x)"},
		Compliant: []string{"log.Info(x)"},
	}}}}

	if _, err := node.NewJevNode(screener, "", 0.3, 0).Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{candidate}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, question := range screener.calls[0] {
		for _, want := range []string{"Code MUST NOT print to stdout.", "fmt.Println(x)", "log.Info(x)"} {
			if !strings.Contains(question, want) {
				t.Errorf("question %q lacks %q", question, want)
			}
		}
	}
}

func TestJevNode_Errors(t *testing.T) {
	tooLarge := &inference.JevError{Status: http.StatusBadRequest, ErrorType: "max_tokens_exceeded"}
	unauthorized := &inference.JevError{Status: http.StatusUnauthorized}
	outage := &inference.JevError{Status: http.StatusServiceUnavailable}

	tests := []struct {
		name     string
		err      error
		failOn   bool
		wantKind node.Kind
		skipped  bool
	}{
		{"skip passes candidates through", outage, false, node.KindUnavailable, true},
		{"skip does not hide a rejected key", unauthorized, false, node.KindUnavailable, false},
		{"fail reports a rejected key", unauthorized, true, node.KindUnavailable, false},
		{"fail reports an unavailable dependency", outage, true, node.KindUnavailable, false},
		{"fail reports input over the size limit as a precondition", tooLarge, true, node.KindPreconditionNotMet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := node.NewJevNode(&fakeScreener{err: tt.err}, "", 0.3, 0)
			s.FailOnError = tt.failOn
			candidates := []node.Candidate{ruledCandidate("a", "no prints"), ruledCandidate("b", "no globals")}

			got, err := s.Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, candidates)

			var skipped *node.Skipped
			if errors.As(err, &skipped) != tt.skipped {
				t.Fatalf("err = %v, want skipped %v", err, tt.skipped)
			}

			if tt.skipped && ids(got) != "a,b" {
				t.Errorf("kept %s, want every candidate unchanged", ids(got))
			}

			if want := errors.Is(tt.err, unauthorized); node.KeyRejected(err) != want {
				t.Errorf("KeyRejected(%v) = %v, want %v", err, !want, want)
			}

			var nodeErr *node.Error
			if !errors.As(err, &nodeErr) || nodeErr.Kind != tt.wantKind {
				t.Errorf("err = %v, want a node error of kind %v", err, tt.wantKind)
			}
		})
	}
}

func TestJevNode_MissingAnswerIsAnError(t *testing.T) {
	screener := screenerFunc(func(map[string]string) map[string]float64 { return map[string]float64{} })
	s := node.NewJevNode(screener, "", 0.3, 0)
	s.FailOnError = true

	if _, err := s.Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{ruledCandidate("a", "no prints")}); err == nil {
		t.Fatal("expected an error when a question gets no answer")
	}
}

type screenerFunc func(questions map[string]string) map[string]float64

func (f screenerFunc) Screen(_ context.Context, _ string, questions map[string]string) (map[string]float64, error) {
	return f(questions), nil
}

func TestJevNode_TopKLeavesPassedThroughCandidates(t *testing.T) {
	s := node.NewJevNode(&fakeScreener{answer: breaks("no prints", "no globals")}, "", 0.3, 1)
	var buf bytes.Buffer
	candidates := []node.Candidate{ruledCandidate("a", "no prints"), ruledCandidate("b", "no globals"), ruledCandidate("c")}

	got, err := s.Apply(t.Context(), fakeFile{text: "code"}, output.New(&buf, true), candidates)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "a,c" {
		t.Errorf("kept %s, want the top screened ADR and the one without rules", ids(got))
	}

	if !strings.Contains(buf.String(), "Kept: ADR c (not scored by this node)") {
		t.Errorf("debug output does not show c passing through:\n%s", buf.String())
	}
}

type sidedFile struct{ sides []string }

func (sidedFile) Path() string                     { return "a.go" }
func (sidedFile) QueryText(context.Context) string { return "" }
func (f sidedFile) Sides() []string                { return f.sides }

func TestJevScreen_ScoresEachCandidateByItsHigherSide(t *testing.T) {
	screener := sideScreener(func(state string, questions map[string]string) (map[string]float64, error) {
		answers := make(map[string]float64, len(questions))
		for name := range questions {
			answers[name] = map[string]float64{"before": 0.8, "after": 0.2}[state]
		}

		return answers, nil
	})

	scores, err := (&node.JevScreen{Screener: screener}).Score(t.Context(), sidedFile{sides: []string{"before", " ", "after"}}, node.NoDebug, []node.Candidate{ruledCandidate("a", "no globals")})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}

	if len(scores) != 1 || scores[0] != 0.8 {
		t.Errorf("scores = %v, want [0.8], the before side's answer", scores)
	}
}

func TestJevScreen_StopsBetweenSidesWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	screener := sideScreener(func(_ string, questions map[string]string) (map[string]float64, error) {
		calls++
		cancel()

		answers := make(map[string]float64, len(questions))
		for name := range questions {
			answers[name] = 0.1
		}

		return answers, nil
	})

	_, err := (&node.JevScreen{Screener: screener}).Score(ctx, sidedFile{sides: []string{"before", "after"}}, node.NoDebug, []node.Candidate{ruledCandidate("a", "no globals")})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("err = %v after %d call(s), want context.Canceled after 1", err, calls)
	}
}

type sideScreener func(state string, questions map[string]string) (map[string]float64, error)

func (f sideScreener) Screen(_ context.Context, state string, questions map[string]string) (map[string]float64, error) {
	return f(state, questions)
}

func TestJevScreen_ASideMissingAnAnswerFails(t *testing.T) {
	screener := sideScreener(func(state string, questions map[string]string) (map[string]float64, error) {
		if state == "after" {
			return map[string]float64{}, nil
		}

		answers := make(map[string]float64, len(questions))
		for name := range questions {
			answers[name] = 0.1
		}

		return answers, nil
	})

	_, err := (&node.JevScreen{Screener: screener}).Score(t.Context(), sidedFile{sides: []string{"before", "after"}}, node.NoDebug, []node.Candidate{ruledCandidate("a", "no globals")})
	if err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Errorf("err = %v, want a missing-answer error for the after side", err)
	}
}
