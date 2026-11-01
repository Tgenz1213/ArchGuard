package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	JobRank  = "rank"
	JobAsk   = "ask"
	JobJudge = "judge"
)

const (
	RunnerCosine = "cosine"
	RunnerJev    = "jev"
	RunnerLLM    = "llm"
)

const JevAPIKeyEnv = "ARCHGUARD_JEV_API_KEY"

const (
	OnErrorSkip = "skip"
	OnErrorFail = "fail"
)

// DefaultPipeline is the pipeline archguard init writes, as the YAML that goes under analysis.
const DefaultPipeline = `pipeline:
  - name: shortlist
    job: rank
    with: cosine

  - name: judge
    job: judge
    with: llm
`

// Node is one validated pipeline node; settings a job doesn't take stay zero.
type Node struct {
	Name string
	Job  string
	With string
	// A rank node's settings fall back to vector_store and analysis; an ask node's come from keep.
	Threshold float64
	TopK      int
	OnError   string
	Model     string
	Question  Question
}

type Question struct {
	Type         string
	ForEach      string
	Instructions string
}

// ParsePipeline validates analysis.pipeline and the jev block. LoadConfig never does, so commands other than check run on any pipeline.
func ParsePipeline(cfg *Config) ([]Node, error) {
	jev, err := parseJev(&cfg.Jev)
	if err != nil {
		return nil, err
	}

	pipeline := resolveAlias(&cfg.Analysis.Pipeline)
	if pipeline.Kind == 0 || pipeline.Tag == "!!null" || pipeline.Kind == yaml.MappingNode {
		return nil, needsInitError(pipeline.Kind == yaml.MappingNode)
	}

	if pipeline.Kind != yaml.SequenceNode {
		return nil, errors.New("analysis.pipeline: must be a list of nodes")
	}

	nodes := make([]Node, 0, len(pipeline.Content))
	for i, item := range pipeline.Content {
		node, err := parseNode(nodeSource{cfg: cfg, jev: jev, position: i + 1, raw: resolveAlias(item)})
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(nodes, func(other Node) bool { return other.Name == node.Name }) {
			return nil, fmt.Errorf("analysis.pipeline: two nodes are named %q; node names must be unique", node.Name)
		}

		nodes = append(nodes, node)
	}

	return nodes, checkOrder(nodes)
}

func needsInitError(retired bool) error {
	problem := "analysis.pipeline: missing"
	if retired {
		problem = "analysis.pipeline: the rank, rerank and pre_judge mapping is no longer supported"
	}

	indented := "  " + strings.ReplaceAll(strings.TrimSuffix(DefaultPipeline, "\n"), "\n", "\n  ")
	indented = strings.ReplaceAll(indented, "\n  \n", "\n\n")

	return fmt.Errorf("%s; run `archguard init` to write the default pipeline, or add it under analysis:\n\nanalysis:\n%s", problem, indented)
}

func checkOrder(nodes []Node) error {
	judges := 0
	for _, node := range nodes {
		if node.Job == JobJudge {
			judges++
		}
	}

	if judges != 1 {
		return fmt.Errorf("analysis.pipeline: needs exactly one judge node, found %d", judges)
	}

	if first := nodes[0]; first.Job != JobRank {
		return fmt.Errorf("analysis.pipeline: the first node, %q, must be a rank node so the candidates are narrowed before anything asks about them", first.Name)
	}

	if last := nodes[len(nodes)-1]; last.Job != JobJudge {
		return fmt.Errorf("analysis.pipeline: node %q comes after the judge node; the judge must be the last node", last.Name)
	}

	return nil
}

type jevSettings struct {
	model string
}

func parseJev(raw *yaml.Node) (jevSettings, error) {
	raw = resolveAlias(raw)
	if raw.Kind == 0 || raw.Tag == "!!null" {
		return jevSettings{}, nil
	}

	if raw.Kind != yaml.MappingNode {
		return jevSettings{}, errors.New("jev: must be a mapping with a model key")
	}

	fields, err := decodeFields("jev", raw)
	if err != nil {
		return jevSettings{}, err
	}

	var settings jevSettings
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		value := fields[key]
		if key != "model" {
			return jevSettings{}, fmt.Errorf("jev: unrecognized key %q (expected model)", key)
		}

		settings.model, err = decodeString("jev", key, &value)
		if err != nil {
			return jevSettings{}, err
		}
	}

	return settings, nil
}
