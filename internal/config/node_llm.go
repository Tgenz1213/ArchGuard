package config

import (
	"fmt"
	"reflect"
	"slices"

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

func llmKeys() []string {
	fields := reflect.TypeFor[LLMConfig]()

	keys := make([]string, 0, fields.NumField())
	for field := range fields.Fields() {
		keys = append(keys, field.Tag.Get("yaml"))
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

	if err := checkKeys(prefix, set, llmKeys()); err != nil {
		return err
	}

	var own LLMConfig
	if err := block.Decode(&own); err != nil {
		return decodeError(prefix, err)
	}

	n.LLM = withKeysSet(cfg.LLM, own, set)
	n.OwnLLM = true

	return n.LLM.CheckCosts(prefix)
}

// withKeysSet copies into base each field of own whose yaml key is in set, so a key own leaves out keeps base's value.
func withKeysSet(base, own LLMConfig, set map[string]yaml.Node) LLMConfig {
	merged := reflect.ValueOf(&base).Elem()
	from := reflect.ValueOf(own)

	for i, field := range slices.Collect(reflect.TypeFor[LLMConfig]().Fields()) {
		if _, ok := set[field.Tag.Get("yaml")]; ok {
			merged.Field(i).Set(from.Field(i))
		}
	}

	return base
}
