package config

import (
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"
)

type Pipeline struct {
	Rank   *StageConfig
	Rerank *StageConfig
}

func (p *Pipeline) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("analysis.pipeline: must be a mapping with rank and rerank keys")
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
		default:
			err = fmt.Errorf("analysis.pipeline: unrecognized stage %q (expected rank or rerank)", key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
