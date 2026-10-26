package config

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ScorerCosine = "cosine"
	ScorerJev    = "jev"
)

const JevAPIKeyEnv = "ARCHGUARD_JEV_API_KEY"

const (
	OnErrorSkip = "skip"
	OnErrorFail = "fail"
)

var (
	scorerNames      = []string{ScorerCosine, ScorerJev}
	rankingScorers   = []string{ScorerCosine}
	screeningScorers = []string{ScorerJev}
	onErrorModes     = []string{OnErrorSkip, OnErrorFail}
)

type StageConfig struct {
	Scorer    string
	Threshold *float64
	TopK      *int
	OnError   string
	// Set only when Scorer is jev.
	Jev *JevOptions
}

type JevOptions struct {
	Model string
	// Read from JevAPIKeyEnv when the config loads, never from archguard.yaml.
	APIKey string
}

func decodeStage(name string, node *yaml.Node) (*StageConfig, error) {
	prefix := "analysis.pipeline." + name

	stage := &StageConfig{}
	if name != stagePreJudge {
		stage.Scorer = ScorerCosine
	}

	node = resolveAlias(node)
	if node.Tag == "!!null" {
		return stage, checkStage(name, stage)
	}

	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: must be a mapping with scorer, threshold, top_k, on_error and options keys", prefix)
	}

	fields, err := decodeFields(prefix, node)
	if err != nil {
		return nil, err
	}

	for _, key := range slices.Sorted(maps.Keys(fields)) {
		field := fields[key]
		if err := stage.decodeKey(prefix, key, resolveAlias(&field)); err != nil {
			return nil, err
		}
	}

	if err := checkStage(name, stage); err != nil {
		return nil, err
	}

	if options, ok := fields["options"]; ok {
		err = stage.decodeOptions(prefix, resolveAlias(&options))
	} else if stage.Scorer == ScorerJev {
		stage.Jev = &JevOptions{}
	}

	return stage, err
}

func (s *StageConfig) decodeKey(prefix, key string, value *yaml.Node) error {
	var err error

	switch key {
	case "scorer":
		s.Scorer, err = decodeScorer(prefix, value)
	case "threshold":
		s.Threshold, err = decodeThreshold(prefix, value)
	case "top_k":
		s.TopK, err = decodeTopK(prefix, value)
	case "on_error":
		s.OnError, err = decodeOnError(prefix, value)
	case "options":
		// Decoded once the scorer is known, since each scorer takes its own options.
	default:
		err = fmt.Errorf("%s: unrecognized key %q (expected scorer, threshold, top_k, on_error or options)", prefix, key)
	}

	return err
}

func checkStage(name string, stage *StageConfig) error {
	prefix := "analysis.pipeline." + name

	if name != stagePreJudge {
		if !slices.Contains(rankingScorers, stage.Scorer) {
			return fmt.Errorf("%s.scorer: %q screens for violations and cannot rank; use it in pre_judge (ranking scorers: %s)", prefix, stage.Scorer, strings.Join(rankingScorers, ", "))
		}

		return nil
	}

	if stage.Scorer == "" {
		return fmt.Errorf("%s.scorer: required (available: %s)", prefix, strings.Join(screeningScorers, ", "))
	}

	if !slices.Contains(screeningScorers, stage.Scorer) {
		return fmt.Errorf("%s.scorer: %q cannot screen for violations (available: %s)", prefix, stage.Scorer, strings.Join(screeningScorers, ", "))
	}

	if stage.Threshold == nil {
		return fmt.Errorf("%s.threshold: required for scorer %s", prefix, stage.Scorer)
	}

	return nil
}

func (s *StageConfig) decodeOptions(prefix string, node *yaml.Node) error {
	prefix += ".options"

	if s.Scorer != ScorerJev {
		return fmt.Errorf("%s: scorer %s takes no options", prefix, s.Scorer)
	}

	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: must be a mapping with a model key", prefix)
	}

	fields, err := decodeFields(prefix, node)
	if err != nil {
		return err
	}

	s.Jev = &JevOptions{}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		field := fields[key]
		value := resolveAlias(&field)
		switch key {
		case "model":
			if value.Tag != "!!str" || value.Decode(&s.Jev.Model) != nil || s.Jev.Model == "" {
				return fmt.Errorf("%s.model: must be a non-empty string", prefix)
			}
		default:
			return fmt.Errorf("%s: unrecognized key %q for scorer jev (expected model)", prefix, key)
		}
	}

	return nil
}

func decodeScorer(prefix string, node *yaml.Node) (string, error) {
	var name string
	if err := node.Decode(&name); err != nil {
		// The yaml cause adds nothing to "must be a string"; callers must not match it.
		return "", fmt.Errorf("%s.scorer: must be a string", prefix)
	}

	if slices.Contains(scorerNames, name) {
		return name, nil
	}

	return "", fmt.Errorf("%s.scorer: unknown scorer %q (available: %s)", prefix, name, strings.Join(scorerNames, ", "))
}

func decodeThreshold(prefix string, node *yaml.Node) (*float64, error) {
	var threshold float64
	if node.Tag == "!!null" || node.Decode(&threshold) != nil {
		return nil, fmt.Errorf("%s.threshold: must be a number between 0 and 1", prefix)
	}

	if math.IsNaN(threshold) || threshold < 0 || threshold > 1 {
		return nil, fmt.Errorf("%s.threshold: %v is out of range, must be between 0 and 1", prefix, node.Value)
	}

	return &threshold, nil
}

func decodeTopK(prefix string, node *yaml.Node) (*int, error) {
	var topK int
	if node.Tag != "!!int" || node.Decode(&topK) != nil {
		return nil, fmt.Errorf("%s.top_k: must be a positive integer", prefix)
	}

	if topK <= 0 {
		return nil, fmt.Errorf("%s.top_k: %d must be a positive integer", prefix, topK)
	}

	return &topK, nil
}

func decodeOnError(prefix string, node *yaml.Node) (string, error) {
	var mode string
	if err := node.Decode(&mode); err != nil || node.Tag == "!!null" {
		return "", fmt.Errorf("%s.on_error: must be one of %s", prefix, strings.Join(onErrorModes, ", "))
	}

	if slices.Contains(onErrorModes, mode) {
		return mode, nil
	}

	return "", fmt.Errorf("%s.on_error: unknown value %q (available: %s)", prefix, mode, strings.Join(onErrorModes, ", "))
}
