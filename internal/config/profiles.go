package config

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

func parseProfiles(raw *yaml.Node) (map[string]LLMConfig, error) {
	raw = resolveAlias(raw)
	if raw.Kind == 0 || raw.Tag == "!!null" {
		return nil, nil
	}

	if raw.Kind != yaml.MappingNode {
		return nil, errors.New("llms: must be a mapping of profile names to llm blocks")
	}

	fields, err := decodeFields("llms", raw)
	if err != nil {
		return nil, err
	}

	profiles := make(map[string]LLMConfig, len(fields))
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		prefix := fmt.Sprintf("llms.%s", name)
		if len(name) > maxNodeNameLength || !nodeNamePattern.MatchString(name) {
			return nil, fmt.Errorf("%s: the profile name must be at most %d letters, digits, - or _", prefix, maxNodeNameLength)
		}

		value := fields[name]

		profile, err := decodeProfile(prefix, resolveAlias(&value))
		if err != nil {
			return nil, err
		}

		profiles[name] = profile
	}

	return profiles, nil
}

func decodeProfile(prefix string, node *yaml.Node) (LLMConfig, error) {
	if node.Kind != yaml.MappingNode {
		return LLMConfig{}, fmt.Errorf("%s: must be a mapping with the keys of an llm block", prefix)
	}

	fields, err := decodeFields(prefix, node)
	if err != nil {
		return LLMConfig{}, err
	}

	if err := checkKeys(prefix, fields, llmKeys()); err != nil {
		return LLMConfig{}, err
	}

	var profile LLMConfig
	if err := node.Decode(&profile); err != nil {
		return LLMConfig{}, decodeError(prefix, err)
	}

	if profile.Provider == "" {
		return LLMConfig{}, fmt.Errorf("%s: provider is required", prefix)
	}

	return profile, nil
}

func llmKeys() []string {
	fields := reflect.TypeFor[LLMConfig]()

	keys := make([]string, 0, fields.NumField())
	for field := range fields.Fields() {
		keys = append(keys, field.Tag.Get("yaml"))
	}

	return keys
}

func (n *Node) decodeLLM(prefix string, fields map[string]yaml.Node, cfg *Config, profiles map[string]LLMConfig) error {
	n.LLM = cfg.LLM

	value, ok := fields["llm"]
	if !ok {
		return nil
	}

	name, err := decodeString(prefix, "llm", &value)
	if err != nil {
		return err
	}

	profile, ok := profiles[name]
	if !ok {
		available := "none; add it under llms:"
		if len(profiles) > 0 {
			available = strings.Join(slices.Sorted(maps.Keys(profiles)), ", ")
		}

		return fmt.Errorf("%s.llm: no profile named %q in llms (available: %s)", prefix, name, available)
	}

	n.Profile = name
	n.LLM = profile

	return nil
}
