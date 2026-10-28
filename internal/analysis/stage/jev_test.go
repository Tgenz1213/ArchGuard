package stage_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
)

type fakeScreener struct {
	answer func(instructions string) float64
	err    error
	calls  []map[string]string
	states []any
}

func (f *fakeScreener) Screen(_ context.Context, state any, questions map[string]string) (map[string]float64, error) {
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

func ruledCandidate(id string, statements ...string) stage.Candidate {
	rules := make(index.Rules, len(statements))
	for i, statement := range statements {
		rules[i] = index.Rule{Statement: statement}
	}

	return stage.Candidate{ADR: &index.ADR{ID: id, Title: "ADR " + id, Rules: rules}, Score: 0.7}
}

func TestJevStage_KeepsADRsWithABrokenRuleInOneCall(t *testing.T) {
	screener := &fakeScreener{answer: breaks("no prints")}
	s := stage.NewJevStage(screener, nil, 0.3, 0)
	candidates := []stage.Candidate{
		ruledCandidate("a", "no globals", "no prints"),
		ruledCandidate("b", "no globals"),
		ruledCandidate("c"),
	}

	got, err := s.Apply(t.Context(), fakeFile{text: "+fmt.Println(x)"}, stage.NoDebug, candidates)
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

func TestJevStage_MakesNoCallWithoutQuestionsOrContent(t *testing.T) {
	tests := []struct {
		name       string
		candidates []stage.Candidate
		text       string
	}{
		{"no candidate has rules", []stage.Candidate{ruledCandidate("a"), ruledCandidate("b")}, "code"},
		{"empty content", []stage.Candidate{ruledCandidate("a", "no prints")}, "  \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			screener := &fakeScreener{answer: breaks()}

			got, err := stage.NewJevStage(screener, nil, 0.3, 0).Apply(t.Context(), fakeFile{text: tt.text}, stage.NoDebug, tt.candidates)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if len(got) != len(tt.candidates) || len(screener.calls) != 0 {
				t.Errorf("kept %s after %d call(s), want every candidate and no call", ids(got), len(screener.calls))
			}
		})
	}
}

func TestJevStage_QuestionCarriesTheRuleAndItsExamples(t *testing.T) {
	screener := &fakeScreener{answer: breaks()}
	candidate := stage.Candidate{ADR: &index.ADR{ID: "a", Rules: index.Rules{{
		Statement: "Code MUST NOT print to stdout.",
		Violating: []string{"fmt.Println(x)"},
		Compliant: []string{"log.Info(x)"},
	}}}}

	if _, err := stage.NewJevStage(screener, nil, 0.3, 0).Apply(t.Context(), fakeFile{text: "code"}, stage.NoDebug, []stage.Candidate{candidate}); err != nil {
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

func TestJevStage_Errors(t *testing.T) {
	tooLarge := &inference.JevError{Status: http.StatusBadRequest, ErrorType: "max_tokens_exceeded"}
	unauthorized := &inference.JevError{Status: http.StatusUnauthorized}

	tests := []struct {
		name     string
		err      error
		failOn   bool
		wantKind stage.Kind
		skipped  bool
	}{
		{"skip passes candidates through", unauthorized, false, stage.KindUnavailable, true},
		{"fail reports an unavailable dependency", unauthorized, true, stage.KindUnavailable, false},
		{"fail reports input over the size limit as a precondition", tooLarge, true, stage.KindPreconditionNotMet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stage.NewJevStage(&fakeScreener{err: tt.err}, nil, 0.3, 0)
			s.FailOnError = tt.failOn
			candidates := []stage.Candidate{ruledCandidate("a", "no prints"), ruledCandidate("b", "no globals")}

			got, err := s.Apply(t.Context(), fakeFile{text: "code"}, stage.NoDebug, candidates)

			var skipped *stage.Skipped
			if errors.As(err, &skipped) != tt.skipped {
				t.Fatalf("err = %v, want skipped %v", err, tt.skipped)
			}

			if tt.skipped && ids(got) != "a,b" {
				t.Errorf("kept %s, want every candidate unchanged", ids(got))
			}

			var stageErr *stage.Error
			if !errors.As(err, &stageErr) || stageErr.Kind != tt.wantKind {
				t.Errorf("err = %v, want a stage error of kind %v", err, tt.wantKind)
			}
		})
	}
}

func TestJevStage_MissingAnswerIsAnError(t *testing.T) {
	screener := screenerFunc(func(map[string]string) map[string]float64 { return map[string]float64{} })
	s := stage.NewJevStage(screener, nil, 0.3, 0)
	s.FailOnError = true

	if _, err := s.Apply(t.Context(), fakeFile{text: "code"}, stage.NoDebug, []stage.Candidate{ruledCandidate("a", "no prints")}); err == nil {
		t.Fatal("expected an error when a question gets no answer")
	}
}

type screenerFunc func(questions map[string]string) map[string]float64

func (f screenerFunc) Screen(_ context.Context, _ any, questions map[string]string) (map[string]float64, error) {
	return f(questions), nil
}

func TestJevStage_TopKLeavesPassedThroughCandidates(t *testing.T) {
	s := stage.NewJevStage(&fakeScreener{answer: breaks("no prints", "no globals")}, nil, 0.3, 1)
	var buf bytes.Buffer
	candidates := []stage.Candidate{ruledCandidate("a", "no prints"), ruledCandidate("b", "no globals"), ruledCandidate("c")}

	got, err := s.Apply(t.Context(), fakeFile{text: "code"}, output.New(&buf, true), candidates)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "a,c" {
		t.Errorf("kept %s, want the top screened ADR and the one without rules", ids(got))
	}

	if !strings.Contains(buf.String(), "Kept: ADR c (not scored by this stage)") {
		t.Errorf("debug output does not show c passing through:\n%s", buf.String())
	}
}

type fakeQuestions struct {
	questions map[string][]string
	err       error
	asked     []string
}

func (f *fakeQuestions) Questions(_ context.Context, adr *index.ADR) ([]string, error) {
	f.asked = append(f.asked, adr.ID)
	return f.questions[adr.ID], f.err
}

func TestJevStage_GeneratedQuestionsScreenADRsWithoutRules(t *testing.T) {
	generated := &fakeQuestions{questions: map[string][]string{"b": {"Does this code print a secret?"}, "c": {"Does this code open a socket?"}}}
	screener := &fakeScreener{answer: breaks("print a secret")}
	var buf bytes.Buffer

	got, err := stage.NewJevStage(screener, generated, 0.3, 0).Apply(
		t.Context(),
		fakeFile{text: "code"},
		output.New(&buf, true),
		[]stage.Candidate{ruledCandidate("a", "no globals"), ruledCandidate("b"), ruledCandidate("c")},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "b" {
		t.Errorf("kept %s, want only b, whose generated question scored 0.9", ids(got))
	}

	if strings.Join(generated.asked, ",") != "b,c" {
		t.Errorf("generated questions for %v, want only the ADRs without rules", generated.asked)
	}

	if !strings.Contains(buf.String(), "Generated question for ADR b: Does this code print a secret?") {
		t.Errorf("debug output does not show the generated question:\n%s", buf.String())
	}
}

func TestJevStage_GenerationFailureFollowsOnError(t *testing.T) {
	generated := &fakeQuestions{err: errors.New("chat down")}
	screener := &fakeScreener{answer: breaks()}
	candidates := []stage.Candidate{ruledCandidate("a")}

	got, err := stage.NewJevStage(screener, generated, 0.3, 0).Apply(t.Context(), fakeFile{text: "code"}, stage.NoDebug, candidates)

	var skipped *stage.Skipped
	if !errors.As(err, &skipped) || ids(got) != "a" || len(screener.calls) != 0 {
		t.Errorf("got %s, err %v, %d Jev call(s); want a skipped stage passing a on with no Jev call", ids(got), err, len(screener.calls))
	}
}

func contentCandidate(id, content string) stage.Candidate {
	return stage.Candidate{ADR: &index.ADR{ID: id, Title: "ADR " + id, Content: content}, Score: 0.7}
}

// Answers each question by whether the ADR text it names, in the state, mentions "prints".
func governsPrints(state map[string]string, questions map[string]string) map[string]float64 {
	answers := make(map[string]float64, len(questions))
	for name := range questions {
		answers[name] = 0.2
		if strings.Contains(state[name], "prints") {
			answers[name] = 0.95
		}
	}

	return answers
}

func TestJevRankStage_RanksEveryCandidateInOneCall(t *testing.T) {
	var states []map[string]string
	var asked []map[string]string

	ranker := stage.NewJevRankStage("rerank", rankScreener(func(state map[string]string, questions map[string]string) map[string]float64 {
		states, asked = append(states, state), append(asked, questions)
		return governsPrints(state, questions)
	}), 0.5, 0)

	candidates := []stage.Candidate{contentCandidate("a", "no globals"), contentCandidate("b", "no prints"), contentCandidate("c", "no prints in handlers")}

	got, err := ranker.Apply(t.Context(), fakeFile{text: "+fmt.Println(x)"}, stage.NoDebug, candidates)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if ids(got) != "b,c" || got[0].Score != 0.95 {
		t.Errorf("kept %s scoring %v, want b and c at 0.95", ids(got), got)
	}

	if len(states) != 1 {
		t.Fatalf("calls = %d, want one", len(states))
	}

	if states[0]["code"] != "+fmt.Println(x)" || states[0]["adr1"] != "no prints" || len(states[0]) != 4 {
		t.Errorf("state = %v, want the judged code and each candidate's full ADR", states[0])
	}

	if asked[0]["adr2"] != "Is `code` governed by the decision in `adr2`?" || len(asked[0]) != 3 {
		t.Errorf("questions = %v, want one per candidate naming its state field", asked[0])
	}
}

func TestJevRankStage_BlankCodePassesCandidatesThrough(t *testing.T) {
	screener := &fakeScreener{answer: breaks()}
	candidates := []stage.Candidate{contentCandidate("a", "x"), contentCandidate("b", "y")}

	got, err := stage.NewJevRankStage("rank", screener, 0.5, 1).Apply(t.Context(), fakeFile{text: " \n"}, stage.NoDebug, candidates)
	if err != nil || ids(got) != "a,b" || len(screener.calls) != 0 {
		t.Errorf("got %s, %v after %d calls; want both candidates unchanged and no call", ids(got), err, len(screener.calls))
	}
}

func TestJevRankStage_Errors(t *testing.T) {
	tooLarge := &inference.JevError{Status: http.StatusBadRequest, ErrorType: "max_tokens_exceeded"}
	unauthorized := &inference.JevError{Status: http.StatusUnauthorized}

	tests := []struct {
		name     string
		err      error
		failOn   bool
		wantKind stage.Kind
		skipped  bool
	}{
		{"skip passes candidates through unranked", unauthorized, false, stage.KindUnavailable, true},
		{"fail reports an unavailable dependency", unauthorized, true, stage.KindUnavailable, false},
		{"fail reports input over the size limit as a precondition", tooLarge, true, stage.KindPreconditionNotMet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stage.NewJevRankStage("rank", &fakeScreener{err: tt.err}, 0.5, 1)
			s.FailOnError = tt.failOn
			candidates := []stage.Candidate{contentCandidate("a", "x"), contentCandidate("b", "y")}

			got, err := s.Apply(t.Context(), fakeFile{text: "code"}, stage.NoDebug, candidates)

			var skipped *stage.Skipped
			if errors.As(err, &skipped) != tt.skipped {
				t.Fatalf("err = %v, want skipped %v", err, tt.skipped)
			}

			if tt.skipped && ids(got) != "a,b" {
				t.Errorf("kept %s, want every candidate unchanged", ids(got))
			}

			var stageErr *stage.Error
			if !errors.As(err, &stageErr) || stageErr.Kind != tt.wantKind || stageErr.Action != "ranking candidates" {
				t.Errorf("err = %v, want a ranking stage error of kind %v", err, tt.wantKind)
			}
		})
	}
}

func TestJevRankStage_MissingAnswerIsAnError(t *testing.T) {
	screener := screenerFunc(func(map[string]string) map[string]float64 { return map[string]float64{} })
	s := stage.NewJevRankStage("rank", screener, 0.5, 0)
	s.FailOnError = true

	if _, err := s.Apply(t.Context(), fakeFile{text: "code"}, stage.NoDebug, []stage.Candidate{contentCandidate("a", "x")}); err == nil {
		t.Fatal("expected an error when a candidate gets no answer")
	}
}

type rankScreener func(state map[string]string, questions map[string]string) map[string]float64

func (f rankScreener) Screen(_ context.Context, state any, questions map[string]string) (map[string]float64, error) {
	return f(state.(map[string]string), questions), nil
}
