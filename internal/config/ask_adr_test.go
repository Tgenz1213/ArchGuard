package config

import "testing"

const governsNode = `
    - name: governs
      job: ask
      with: jev
      preset: governs
`

func TestParsePipeline_GovernsPreset(t *testing.T) {
	nodes, err := parseFromYAML(t, "", pipelineWith(governsNode))
	if err != nil {
		t.Fatal(err)
	}

	want := Node{
		Name: "governs", Job: JobAsk, With: RunnerJev, Threshold: 0.5, TopK: 3, OnError: OnErrorSkip,
		Question: Question{Type: QuestionNoul, ForEach: ForEachADR, Instructions: GovernsInstructions},
	}
	if nodes[1] != want {
		t.Errorf("governs = %+v, want %+v", nodes[1], want)
	}
}

func TestParsePipeline_GovernsTakesKeepModelAndOnError(t *testing.T) {
	nodes, err := parseFromYAML(t, "", pipelineWith(governsNode+"      model: jev-2\n      keep:\n        threshold: 0.7\n        top_k: 5\n      on_error: fail\n"))
	if err != nil {
		t.Fatal(err)
	}

	if got := nodes[1]; got.Threshold != 0.7 || got.TopK != 5 || got.Model != "jev-2" || got.OnError != OnErrorFail {
		t.Errorf("governs = %+v, want keep 0.7 and 5, model jev-2, on_error fail", got)
	}
}

func TestParsePipeline_AskPerADRWithItsOwnQuestion(t *testing.T) {
	nodes, err := parseFromYAML(t, "", pipelineWith(`
    - name: own
      job: ask
      with: jev
      type: noul
      for_each: adr
      instructions: "Does {adr} apply to {code}?"
      keep:
        threshold: 0.4
`))
	if err != nil {
		t.Fatal(err)
	}

	if got := nodes[1].Question; got != (Question{Type: QuestionNoul, ForEach: ForEachADR, Instructions: "Does {adr} apply to {code}?"}) {
		t.Errorf("question = %+v", got)
	}

	if nodes[1].TopK != 0 {
		t.Errorf("top_k = %d, want none without keep.top_k", nodes[1].TopK)
	}
}

func TestParsePipeline_FirstAskDefaultsToFail(t *testing.T) {
	for _, extra := range []string{"", "      on_error: fail\n"} {
		nodes, err := parseFromYAML(t, "", "  pipeline:"+governsNode+extra+judgeNode)
		if err != nil {
			t.Fatalf("%q: %v", extra, err)
		}

		if nodes[0].OnError != OnErrorFail {
			t.Errorf("%q: on_error = %s, want fail on a first ask node", extra, nodes[0].OnError)
		}
	}
}
