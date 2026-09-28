package index

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"
)

const DefaultRulesHeading = "Rules"

type Rule struct {
	Statement string   `json:"statement"`
	Violating []string `json:"violating,omitempty"`
	Compliant []string `json:"compliant,omitempty"`
}

type Rules []Rule

func (r Rules) Value() (driver.Value, error) {
	if len(r) == 0 {
		return nil, nil
	}

	data, err := json.Marshal([]Rule(r))
	if err != nil {
		return nil, err
	}

	return string(data), nil
}

func (r *Rules) Scan(src any) error {
	var data []byte
	switch v := src.(type) {
	case nil:
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("unsupported scan type %T for Rules", src)
	}

	if len(data) == 0 {
		*r = nil
		return nil
	}

	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return err
	}

	*r = Rules(rules)
	return nil
}

func isNullNode(node *yaml.Node) bool {
	return node.Kind == 0 || (node.Kind == yaml.ScalarNode && node.Tag == "!!null")
}

func resolveAlias(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return node.Alias
	}

	return node
}

func decodeFrontMatterRules(node *yaml.Node) (Rules, error) {
	node = resolveAlias(node)
	if isNullNode(node) {
		return nil, nil
	}

	if node.Kind != yaml.SequenceNode {
		return nil, errors.New("rules must be a list")
	}

	rules := make(Rules, 0, len(node.Content))
	for i, item := range node.Content {
		item = resolveAlias(item)
		if item.Kind == yaml.ScalarNode && !isNullNode(item) {
			statement, err := decodeStatement(item)
			if err != nil {
				return nil, fmt.Errorf("rule %d: %w", i+1, err)
			}

			if statement == "" {
				return nil, fmt.Errorf("rule %d: statement is required", i+1)
			}

			rules = append(rules, Rule{Statement: statement})

			continue
		}

		rule, err := decodeRule(item)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}

		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return nil, nil
	}

	return rules, nil
}

func decodeRule(node *yaml.Node) (Rule, error) {
	if node.Kind != yaml.MappingNode {
		return Rule{}, errors.New("must be a string or a mapping")
	}

	var rule Rule
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, resolveAlias(node.Content[i+1])
		var err error
		switch key {
		case "statement":
			rule.Statement, err = decodeStatement(value)
		case "violating":
			rule.Violating, err = decodeExamples(key, value)
		case "compliant":
			rule.Compliant, err = decodeExamples(key, value)
		default:
			err = fmt.Errorf("unknown key %q", key)
		}

		if err != nil {
			return Rule{}, err
		}
	}

	if rule.Statement == "" {
		return Rule{}, errors.New("statement is required")
	}

	return rule, nil
}

func decodeStatement(node *yaml.Node) (string, error) {
	if isNullNode(node) {
		return "", nil
	}

	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", errors.New("statement must be a string")
	}

	return strings.TrimSpace(node.Value), nil
}

func decodeExamples(field string, node *yaml.Node) ([]string, error) {
	if isNullNode(node) {
		return nil, nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		return exampleList(field, []*yaml.Node{node})
	case yaml.SequenceNode:
		return exampleList(field, node.Content)
	}

	return nil, fmt.Errorf("%s must be a string or a list of strings", field)
}

func exampleList(field string, nodes []*yaml.Node) ([]string, error) {
	if len(nodes) == 0 {
		return nil, nil
	}

	examples := make([]string, 0, len(nodes))
	for _, n := range nodes {
		n = resolveAlias(n)
		if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
			return nil, fmt.Errorf("%s must be a string or a list of strings", field)
		}

		example := strings.TrimSpace(n.Value)
		if example == "" {
			return nil, fmt.Errorf("%s contains an empty example", field)
		}

		examples = append(examples, example)
	}

	return examples, nil
}

func extractBodyRules(body, heading string) (Rules, error) {
	src := []byte(body)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))

	var rules Rules
	inSection, startLevel := false, 0
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		switch n := node.(type) {
		case *ast.Heading:
			if inSection && n.Level <= startLevel {
				return rules, nil
			}

			if !inSection && strings.EqualFold(headingText(n, src), normalizeHeading(heading)) {
				inSection, startLevel = true, n.Level
			}
		case *ast.List:
			if !inSection {
				continue
			}

			for item := n.FirstChild(); item != nil; item = item.NextSibling() {
				statement := itemStatement(item, src)
				if statement == "" {
					return nil, fmt.Errorf("rule %d: bullet has no statement text", len(rules)+1)
				}

				rules = append(rules, Rule{Statement: statement})
			}
		}
	}

	return rules, nil
}

func headingText(n *ast.Heading, src []byte) string {
	return normalizeHeading(inlineText(n, src))
}

func inlineText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch t := child.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
		case *ast.String:
			b.Write(t.Value)
		}

		return ast.WalkContinue, nil
	})

	return b.String()
}

func normalizeHeading(heading string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(heading), ":"))
}

var taskMarker = regexp.MustCompile(`^\[[ xX]\](\s+|$)`)

func itemStatement(item ast.Node, src []byte) string {
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.(type) {
		case *ast.TextBlock, *ast.Paragraph:
			return taskMarker.ReplaceAllString(blockText(child, src), "")
		}
	}

	return ""
}

func blockText(n ast.Node, src []byte) string {
	lines := n.Lines()
	parts := make([]string, 0, lines.Len())
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		parts = append(parts, string(segment.Value(src)))
	}

	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func frontMatterRules(node *yaml.Node) (Rules, error) {
	rules, err := decodeFrontMatterRules(node)
	if err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}

	return rules, nil
}

func bodyRules(body, heading string) (Rules, error) {
	rules, err := extractBodyRules(body, heading)
	if err != nil {
		return nil, fmt.Errorf("%q section: %w", heading, err)
	}

	return rules, nil
}
