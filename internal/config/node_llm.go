package config

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

// CheckCosts requires both chat prices or neither, since a cost from one side alone would understate the run.
func (llm LLMConfig) CheckCosts(block string) error {
	prices := []struct {
		key   string
		value *float64
	}{
		{block + ".cost_per_1k_input_tokens", llm.CostPer1KInputTokens},
		{block + ".cost_per_1k_output_tokens", llm.CostPer1KOutputTokens},
	}
	for _, price := range prices {
		if price.value != nil && *price.value < 0 {
			return fmt.Errorf("%s cannot be negative, got %v", price.key, *price.value)
		}
	}

	if (llm.CostPer1KInputTokens == nil) != (llm.CostPer1KOutputTokens == nil) {
		return fmt.Errorf("set both %s.cost_per_1k_input_tokens and %s.cost_per_1k_output_tokens, or neither", block, block)
	}

	return nil
}

func llmKeys(job string) []string {
	fields := reflect.TypeFor[LLMConfig]()

	keys := make([]string, 0, fields.NumField())
	for field := range fields.Fields() {
		key := field.Tag.Get("yaml")
		if key == "system_prompt" && job == JobSuggest {
			continue
		}

		keys = append(keys, key)
	}

	return keys
}

// decodeLLM gives the node the llm block, with the keys its own llm block sets replacing the llm block's.
func (n *Node) decodeLLM(prefix string, fields map[string]yaml.Node, cfg *Config) error {
	n.LLM = cfg.LLM

	value, ok := fields["llm"]
	if !ok {
		return nil
	}

	prefix += ".llm"

	block := resolveAlias(&value)
	if block.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: must be a mapping with keys of the llm block, such as model or max_tokens", prefix)
	}

	set, err := decodeFields(prefix, block)
	if err != nil {
		return err
	}

	if err := checkKeys(prefix, set, llmKeys(n.Job)); err != nil {
		return err
	}

	n.LLM = cfg.LLM.clone()
	if err := block.Decode(&n.LLM); err != nil {
		return decodeError(prefix, err)
	}

	if _, ok := set["provider"]; ok && n.LLM.Provider == "" {
		return fmt.Errorf("%s.provider: cannot be empty; leave it out to use llm.provider", prefix)
	}

	if !setsAPrice(set) {
		return nil
	}

	return n.LLM.CheckCosts(prefix)
}

func setsAPrice(set map[string]yaml.Node) bool {
	_, input := set["cost_per_1k_input_tokens"]
	_, output := set["cost_per_1k_output_tokens"]

	return input || output
}

// Same reports whether two llm blocks set the same values, comparing what pointers point to.
func (llm LLMConfig) Same(other LLMConfig) bool {
	return reflect.DeepEqual(llm, other)
}

// clone copies the pointer fields so decoding a node's block over it never writes through to the llm block.
func (llm LLMConfig) clone() LLMConfig {
	llm.Temperature = clonePtr(llm.Temperature)
	llm.CostPer1KInputTokens = clonePtr(llm.CostPer1KInputTokens)
	llm.CostPer1KOutputTokens = clonePtr(llm.CostPer1KOutputTokens)

	return llm
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}

	return new(*p)
}
