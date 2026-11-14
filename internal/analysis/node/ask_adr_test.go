package node_test

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis/node"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
)

type stateAsker struct {
	answer func(state, question string) float64
	err    error
	states []string
	asked  []map[string]string
}

func (s *stateAsker) Ask(_ context.Context, state string, questions map[string]string) (map[string]float64, error) {
	s.states = append(s.states, state)
	s.asked = append(s.asked, questions)

	if s.err != nil {
		return nil, s.err
	}

	answers := make(map[string]float64, len(questions))
	for name, question := range questions {
		answers[name] = s.answer(state, question)
	}

	return answers, nil
}

func contentCandidate(id, content string) node.Candidate {
	return node.Candidate{ADR: &index.ADR{ID: id, Title: "ADR " + id, Content: content}, Score: 0.7}
}

// byContent answers each question with the probability listed for the ADR content it ends with.
func byContent(probabilities map[string]float64) func(string, string) float64 {
	return func(_, question string) float64 {
		for content, probability := range probabilities {
			if strings.HasSuffix(question, " "+content) {
				return probability
			}
		}

		return 0
	}
}

func TestADRAsk_AsksOncePerCandidateInOneCall(t *testing.T) {
	asker := &stateAsker{answer: byContent(nil)}
	scorer := &node.ADRAsk{Asker: asker, Instructions: config.GovernsInstructions}

	if _, err := scorer.Score(t.Context(), fakeFile{text: "fmt.Println(x)"}, node.NoDebug, []node.Candidate{contentCandidate("a", "Never print."), contentCandidate("b", "Use Go.")}); err != nil {
		t.Fatalf("Score: %v", err)
	}

	if len(asker.states) != 1 || asker.states[0] != "fmt.Println(x)" {
		t.Fatalf("states = %q, want one call about the file alone", asker.states)
	}

	wantQuestions := map[string]string{
		"adr0": "Is this code governed by the following decision? Never print.",
		"adr1": "Is this code governed by the following decision? Use Go.",
	}
	if !maps.Equal(asker.asked[0], wantQuestions) {
		t.Errorf("questions = %v, want %v", asker.asked[0], wantQuestions)
	}
}

func TestADRAskNode_KeepsByThresholdThenTopKBestFirst(t *testing.T) {
	asker := &stateAsker{answer: byContent(map[string]float64{"a": 0.6, "b": 0.9, "c": 0.4, "d": 0.7})}
	n := node.NewADRAskNode(asker, config.GovernsInstructions, 0.5, 2)

	kept, err := n.Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{
		contentCandidate("a", "a"), contentCandidate("b", "b"), contentCandidate("c", "c"), contentCandidate("d", "d"),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(kept) != 2 || kept[0].ADR.ID != "b" || kept[1].ADR.ID != "d" || kept[0].Score != 0.9 {
		t.Errorf("kept = %+v, want b (0.9) then d: c is below 0.5 and a is cut by top_k", kept)
	}
}

func TestADRAsk_ScoresTheHigherSide(t *testing.T) {
	asker := &stateAsker{answer: func(state, question string) float64 {
		return map[string]map[string]float64{
			"before": {"ADR-A": 0.8, "ADR-B": 0.1},
			"after":  {"ADR-A": 0.2, "ADR-B": 0.6},
		}[state][question[strings.LastIndex(question, " ")+1:]]
	}}

	scores, err := (&node.ADRAsk{Asker: asker, Instructions: config.GovernsInstructions}).Score(t.Context(), sidedFile{sides: []string{"before", "after"}}, node.NoDebug, []node.Candidate{contentCandidate("a", "ADR-A"), contentCandidate("b", "ADR-B")})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}

	if len(asker.states) != 2 || scores[0] != 0.8 || scores[1] != 0.6 {
		t.Errorf("scores = %v after %d calls, want [0.8 0.6] from one call per side", scores, len(asker.states))
	}
}

func TestADRAsk_SkipsAnEmptySide(t *testing.T) {
	asker := &stateAsker{answer: byContent(map[string]float64{"a": 0.7})}

	scores, err := (&node.ADRAsk{Asker: asker, Instructions: config.GovernsInstructions}).Score(t.Context(), sidedFile{sides: []string{" \n", "after"}}, node.NoDebug, []node.Candidate{contentCandidate("a", "a")})
	if err != nil || len(asker.states) != 1 || scores[0] != 0.7 {
		t.Errorf("scores = %v, err %v after %d calls; want one call for the side that exists", scores, err, len(asker.states))
	}
}

func TestADRAskNode_AFileWithNoTextKeepsNothing(t *testing.T) {
	asker := &stateAsker{answer: byContent(nil)}
	n := node.NewADRAskNode(asker, config.GovernsInstructions, 0, 1)

	kept, err := n.Apply(t.Context(), sidedFile{sides: []string{""}}, node.NoDebug, []node.Candidate{contentCandidate("a", "a"), contentCandidate("b", "b")})
	if err != nil || len(kept) != 0 || len(asker.states) != 0 {
		t.Errorf("kept = %d, err %v after %d calls; want none kept without asking, so a first node never passes every ADR on", len(kept), err, len(asker.states))
	}
}

func TestADRAsk_EmptyInstructionsAskTheGovernsQuestion(t *testing.T) {
	asker := &stateAsker{answer: byContent(nil)}

	if _, err := (&node.ADRAsk{Asker: asker}).Score(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{contentCandidate("a", "a")}); err != nil {
		t.Fatalf("Score: %v", err)
	}

	if got := asker.asked[0]["adr0"]; got != "Is this code governed by the following decision? a" {
		t.Errorf("question = %q, want the governs question", got)
	}
}

func TestADRAsk_AMissingAnswerIsAnError(t *testing.T) {
	_, err := (&node.ADRAsk{Asker: askerWithout("adr1"), Instructions: config.GovernsInstructions}).Score(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{contentCandidate("a", "a"), contentCandidate("b", "b")})
	if err == nil || !strings.Contains(err.Error(), "no answer for question adr1") {
		t.Errorf("err = %v, want the missing answer named", err)
	}
}

type askerWithout string

func (a askerWithout) Ask(_ context.Context, _ string, questions map[string]string) (map[string]float64, error) {
	answers := make(map[string]float64, len(questions))
	for name := range questions {
		if name != string(a) {
			answers[name] = 0.5
		}
	}

	return answers, nil
}

func TestADRAsk_ErrorsKeepTheirMeaning(t *testing.T) {
	tooLarge := &inference.JevError{Status: 400, ErrorType: "max_tokens_exceeded"}
	rejected := &inference.JevError{Status: 401}

	_, err := (&node.ADRAsk{Asker: &stateAsker{err: tooLarge}, Instructions: config.GovernsInstructions}).Score(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{contentCandidate("a", "a")})

	nodeErr, ok := errors.AsType[*node.Error](err)
	if !ok || nodeErr.Kind != node.KindPreconditionNotMet || !errors.Is(err, inference.ErrInputTooLarge) || !strings.Contains(err.Error(), "candidate ADRs") {
		t.Errorf("err = %v, want a too-large precondition error naming the candidate ADRs", err)
	}

	n := node.NewADRAskNode(&stateAsker{err: rejected}, config.GovernsInstructions, 0.5, 3)

	if _, err := n.Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, []node.Candidate{contentCandidate("a", "a")}); !node.KeyRejected(err) {
		t.Errorf("err = %v, want a rejected key that even on_error: skip doesn't pass over", err)
	}
}

func TestAskNodes_AFileTooLargeForJevFollowsOnError(t *testing.T) {
	tooLarge := &inference.JevError{Status: 400, ErrorType: "max_tokens_exceeded"}
	candidates := []node.Candidate{ruledCandidate("a", "no globals"), ruledCandidate("b", "no prints")}

	for _, n := range []node.Node{node.NewAskNode(&stateAsker{err: tooLarge}, "", 0.3, 0), node.NewADRAskNode(&stateAsker{err: tooLarge}, "", 0.5, 1)} {
		got, err := n.Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, candidates)
		if skipped, ok := errors.AsType[*node.Skipped](err); !ok || skipped == nil || ids(got) != "a,b" {
			t.Errorf("%T under skip kept %s with err %v, want every candidate passed on", n.Scorer, ids(got), err)
		}

		n.FailOnError = true

		got, err = n.Apply(t.Context(), fakeFile{text: "code"}, node.NoDebug, candidates)
		if nodeErr, ok := errors.AsType[*node.Error](err); !ok || nodeErr.Kind != node.KindPreconditionNotMet || got != nil {
			t.Errorf("%T under fail kept %s with err %v, want a precondition_not_met failure", n.Scorer, ids(got), err)
		}
	}
}
