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
		if typeErr, ok := errors.AsType[*yaml.TypeError](err); ok {
			return nil, fmt.Errorf("%s: %s", prefix, strings.Join(typeErr.Errors, "; "))
		}
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	return fields, nil
}

func resolveAlias(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return node.Alias
	}
	return node
}
