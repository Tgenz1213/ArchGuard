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
	if p == nil || p.PreJudge == nil || p.PreJudge.Jev == nil {
		return nil
	}

	key := getenv(JevAPIKeyEnv)
	if key == "" {
		return fmt.Errorf("analysis.pipeline.%s: scorer jev needs the %s environment variable", stagePreJudge, JevAPIKeyEnv)
	}

	p.PreJudge.Jev.APIKey = key

	return nil
}
