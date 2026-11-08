package config

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func decodeFields(prefix string, node *yaml.Node) (map[string]yaml.Node, error) {
	var fields map[string]yaml.Node
	if err := node.Decode(&fields); err != nil {
		return nil, decodeError(prefix, err)
	}

	return fields, nil
}

func decodeError(prefix string, err error) error {
	if typeErr, ok := errors.AsType[*yaml.TypeError](err); ok {
		// Flattened to one line for the user; callers must not match the yaml cause.
		return fmt.Errorf("%s: %s", prefix, strings.Join(typeErr.Errors, "; "))
	}

	return fmt.Errorf("%s: %w", prefix, err)
}

func resolveAlias(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return node.Alias
	}

	return node
}
