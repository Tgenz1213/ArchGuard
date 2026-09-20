package config

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const ScorerCosine = "cosine"

const (
	OnErrorSkip = "skip"
	OnErrorFail = "fail"
)

var (
	scorerNames  = []string{ScorerCosine}
	onErrorModes = []string{OnErrorSkip, OnErrorFail}
)

type StageConfig struct {
	Scorer    string
	Threshold *float64
	TopK      *int
	OnError   string
}

func decodeStage(name string, node *yaml.Node) (*StageConfig, error) {
	prefix := "analysis.pipeline." + name
	node = resolveAlias(node)
	if node.Tag == "!!null" {
		return &StageConfig{Scorer: ScorerCosine}, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: must be a mapping with scorer, threshold, top_k and on_error keys", prefix)
	}

	fields, err := decodeFields(prefix, node)
	if err != nil {
		return nil, err
	}

	stage := &StageConfig{Scorer: ScorerCosine}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		field := fields[key]
		value := resolveAlias(&field)
		switch key {
		case "scorer":
			stage.Scorer, err = decodeScorer(prefix, value)
		case "threshold":
			stage.Threshold, err = decodeThreshold(prefix, value)
		case "top_k":
			stage.TopK, err = decodeTopK(prefix, value)
		case "on_error":
			stage.OnError, err = decodeOnError(prefix, value)
		default:
			err = fmt.Errorf("%s: unrecognized key %q (expected scorer, threshold, top_k or on_error)", prefix, key)
		}
		if err != nil {
			return nil, err
		}
	}
	return stage, nil
}

func decodeScorer(prefix string, node *yaml.Node) (string, error) {
	var name string
	if err := node.Decode(&name); err != nil {
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
