package config

import (
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

const stagePreJudge = "pre_judge"

type Pipeline struct {
	Rank     *StageConfig
	Rerank   *StageConfig
	PreJudge *StageConfig
}

func (p *Pipeline) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("analysis.pipeline: must be a mapping with rank, rerank and pre_judge keys")
	}

	fields, err := decodeFields("analysis.pipeline", node)
	if err != nil {
		return err
	}

	for _, key := range slices.Sorted(maps.Keys(fields)) {
		value := fields[key]
		switch key {
		case "rank":
			p.Rank, err = decodeStage("rank", &value)
		case "rerank":
			p.Rerank, err = decodeStage("rerank", &value)
		case stagePreJudge:
			p.PreJudge, err = decodeStage(stagePreJudge, &value)
		default:
			err = fmt.Errorf("analysis.pipeline: unrecognized stage %q (expected rank, rerank or pre_judge)", key)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// Credentials come from the environment only, so a key never sits in archguard.yaml.
func (p *Pipeline) loadCredentials(getenv func(string) string) error {
	if p == nil {
		return nil
	}

	stages := map[string]*StageConfig{"rank": p.Rank, "rerank": p.Rerank, stagePreJudge: p.PreJudge}
	for _, name := range slices.Sorted(maps.Keys(stages)) {
		stage := stages[name]
		if stage == nil || stage.Jev == nil {
			continue
		}

		key := getenv(JevAPIKeyEnv)
		if key == "" {
			return fmt.Errorf("analysis.pipeline.%s: scorer jev needs the %s environment variable", name, JevAPIKeyEnv)
		}

		stage.Jev.APIKey = key
	}

	return nil
}

// JevIsOnlyRanker reports whether no cosine stage narrows the candidates, so a skipped Jev ranker sends every scope-matched ADR to the LLM.
func (p *Pipeline) JevIsOnlyRanker() bool {
	if p == nil || p.Rank == nil || p.Rank.Scorer != ScorerJev {
		return false
	}

	return p.Rerank == nil || p.Rerank.Scorer == ScorerJev
}
