package config

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	QuestionNoul = "noul"
	ForEachRule  = "rule"
)

const PresetBreaksARule = "breaks-a-rule"

const BreaksARuleInstructions = "Does this code break the following rule? {rule}"

const breaksARuleThreshold = 0.3

const maxNodeNameLength = 64

var (
	jobs          = []string{JobRank, JobAsk, JobJudge}
	onErrorModes  = []string{OnErrorSkip, OnErrorFail}
	runnersForJob = map[string][]string{
		JobRank:  {RunnerCosine},
		JobAsk:   {RunnerJev},
		JobJudge: {RunnerLLM},
	}
	keysForJob = map[string][]string{
		JobRank:  {"name", "job", "with", "threshold", "top_k", "on_error"},
		JobAsk:   {"name", "job", "with", "model", "preset", "type", "for_each", "instructions", "keep", "on_error"},
		JobJudge: {"name", "job", "with"},
	}
	nodeNamePattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	placeholderPattern = regexp.MustCompile(`\{([a-z_]+)\}`)
)

type nodeSource struct {
	cfg      *Config
	jev      jevSettings
	position int
	raw      *yaml.Node
}

func parseNode(src nodeSource) (Node, error) {
	prefix := fmt.Sprintf("analysis.pipeline node %d", src.position)
	if src.raw.Kind != yaml.MappingNode {
		return Node{}, fmt.Errorf("%s: must be a mapping with name, job and with keys", prefix)
	}

	fields, err := decodeFields(prefix, src.raw)
	if err != nil {
		return Node{}, err
	}

	name, err := decodeName(prefix, fields)
	if err != nil {
		return Node{}, err
	}

	prefix = fmt.Sprintf("analysis.pipeline node %q", name)
	node := Node{Name: name}

	if node.Job, err = decodeChoice(prefix, "job", fields, jobs); err != nil {
		return Node{}, err
	}

	if node.With, err = decodeChoice(prefix, "with", fields, runnersForJob[node.Job]); err != nil {
		return Node{}, err
	}

	if err := checkKeys(prefix, fields, keysForJob[node.Job]); err != nil {
		return Node{}, err
	}

	switch node.Job {
	case JobRank:
		err = node.decodeRank(prefix, fields, src.cfg)
	case JobAsk:
		err = node.decodeAsk(prefix, fields, src.jev)
	}

	return node, err
}

func decodeName(prefix string, fields map[string]yaml.Node) (string, error) {
	value, ok := fields["name"]
	if !ok {
		return "", fmt.Errorf("%s: name is required", prefix)
	}

	name, err := decodeString(prefix, "name", &value)
	if err != nil {
		return "", err
	}

	if len(name) > maxNodeNameLength || !nodeNamePattern.MatchString(name) {
		return "", fmt.Errorf("%s.name: %q must be at most %d letters, digits, - or _", prefix, name, maxNodeNameLength)
	}

	return name, nil
}

func decodeChoice(prefix, key string, fields map[string]yaml.Node, allowed []string) (string, error) {
	value, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%s: %s is required (available: %s)", prefix, key, strings.Join(allowed, ", "))
	}

	choice, err := decodeString(prefix, key, &value)
	if err != nil {
		return "", err
	}

	if !slices.Contains(allowed, choice) {
		return "", fmt.Errorf("%s.%s: unknown value %q (available: %s)", prefix, key, choice, strings.Join(allowed, ", "))
	}

	return choice, nil
}

func checkKeys(prefix string, fields map[string]yaml.Node, allowed []string) error {
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("%s: %q is a setting not allowed on the node (allowed: %s)", prefix, key, strings.Join(allowed, ", "))
		}
	}

	return nil
}

func (n *Node) decodeRank(prefix string, fields map[string]yaml.Node, cfg *Config) error {
	n.Threshold = cfg.VectorStore.SimilarityThreshold
	n.TopK = cfg.Analysis.RelevantADRLimit()

	if value, ok := fields["threshold"]; ok {
		threshold, err := decodeThreshold(prefix, resolveAlias(&value))
		if err != nil {
			return err
		}

		n.Threshold = *threshold
	}

	if value, ok := fields["top_k"]; ok {
		topK, err := decodeTopK(prefix, resolveAlias(&value))
		if err != nil {
			return err
		}

		n.TopK = *topK
	}

	return n.decodeOnErrorField(prefix, fields)
}

func (n *Node) decodeOnErrorField(prefix string, fields map[string]yaml.Node) error {
	n.OnError = OnErrorSkip

	value, ok := fields["on_error"]
	if !ok {
		return nil
	}

	var err error
	n.OnError, err = decodeOnError(prefix, resolveAlias(&value))

	return err
}

func (n *Node) decodeAsk(prefix string, fields map[string]yaml.Node, jev jevSettings) error {
	n.Model = jev.model

	if value, ok := fields["model"]; ok {
		var err error
		if n.Model, err = decodeString(prefix, "model", &value); err != nil {
			return err
		}
	}

	if err := n.decodeQuestion(prefix, fields); err != nil {
		return err
	}

	if err := n.decodeKeep(prefix, fields); err != nil {
		return err
	}

	return n.decodeOnErrorField(prefix, fields)
}

func (n *Node) decodeQuestion(prefix string, fields map[string]yaml.Node) error {
	value, hasPreset := fields["preset"]
	if !hasPreset {
		return n.decodeOwnQuestion(prefix, fields)
	}

	for _, key := range []string{"type", "for_each", "instructions"} {
		if _, ok := fields[key]; ok {
			return fmt.Errorf("%s: %s can't be set beside a preset", prefix, key)
		}
	}

	preset, err := decodeString(prefix, "preset", &value)
	if err != nil {
		return err
	}

	if preset != PresetBreaksARule {
		return fmt.Errorf("%s.preset: unknown preset %q (available: %s)", prefix, preset, PresetBreaksARule)
	}

	n.Question = Question{Type: QuestionNoul, ForEach: ForEachRule, Instructions: BreaksARuleInstructions}
	n.Threshold = breaksARuleThreshold

	return nil
}

func (n *Node) decodeOwnQuestion(prefix string, fields map[string]yaml.Node) error {
	for _, key := range []string{"type", "for_each", "instructions"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("%s: needs a preset, or all of type, for_each and instructions (%s is missing)", prefix, key)
		}
	}

	var err error
	if n.Question.Type, err = decodeSupported(prefix, "type", fields, QuestionNoul, []string{"choice", "score"}); err != nil {
		return err
	}

	if n.Question.ForEach, err = decodeSupported(prefix, "for_each", fields, ForEachRule, []string{"adr"}); err != nil {
		return err
	}

	value := fields["instructions"]
	if n.Question.Instructions, err = decodeString(prefix, "instructions", &value); err != nil {
		return err
	}

	return checkRulePlaceholders(prefix, n.Question.Instructions)
}

// decodeSupported accepts only the supported value, and names a planned value as not supported yet rather than unknown.
func decodeSupported(prefix, key string, fields map[string]yaml.Node, supported string, planned []string) (string, error) {
	value := fields[key]

	got, err := decodeString(prefix, key, &value)
	if err != nil {
		return "", err
	}

	if slices.Contains(planned, got) {
		return "", fmt.Errorf("%s.%s: %q is not supported yet (available: %s)", prefix, key, got, supported)
	}

	if got != supported {
		return "", fmt.Errorf("%s.%s: unknown value %q (available: %s)", prefix, key, got, supported)
	}

	return got, nil
}

func checkRulePlaceholders(prefix, instructions string) error {
	found := false
	for _, match := range placeholderPattern.FindAllStringSubmatch(instructions, -1) {
		switch match[1] {
		case "rule":
			found = true
		case "code", "adr":
			return fmt.Errorf("%s.instructions: {%s} isn't allowed with for_each: rule, since the code is what is asked about; write \"this code\" instead", prefix, match[1])
		default:
			return fmt.Errorf("%s.instructions: unknown placeholder {%s} (available: {rule})", prefix, match[1])
		}
	}

	if !found {
		return fmt.Errorf("%s.instructions: must contain {rule}", prefix)
	}

	return nil
}

func (n *Node) decodeKeep(prefix string, fields map[string]yaml.Node) error {
	_, hasPreset := fields["preset"]

	value, ok := fields["keep"]
	if !ok {
		if hasPreset {
			return nil
		}

		return fmt.Errorf("%s: keep.threshold is required without a preset", prefix)
	}

	keepPrefix := prefix + ".keep"

	keep := resolveAlias(&value)
	if keep.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: must be a mapping with threshold and top_k keys", keepPrefix)
	}

	keepFields, err := decodeFields(keepPrefix, keep)
	if err != nil {
		return err
	}

	if err := checkKeys(keepPrefix, keepFields, []string{"threshold", "top_k"}); err != nil {
		return err
	}

	return n.decodeKeepFields(keepPrefix, keepFields, hasPreset)
}

func (n *Node) decodeKeepFields(prefix string, fields map[string]yaml.Node, hasPreset bool) error {
	value, ok := fields["threshold"]
	if !ok && !hasPreset {
		return fmt.Errorf("%s: threshold is required without a preset", prefix)
	}

	if ok {
		threshold, err := decodeThreshold(prefix, resolveAlias(&value))
		if err != nil {
			return err
		}

		n.Threshold = *threshold
	}

	if value, ok := fields["top_k"]; ok {
		topK, err := decodeTopK(prefix, resolveAlias(&value))
		if err != nil {
			return err
		}

		n.TopK = *topK
	}

	return nil
}

func decodeString(prefix, key string, node *yaml.Node) (string, error) {
	node = resolveAlias(node)

	var value string
	if node.Tag != "!!str" || node.Decode(&value) != nil || value == "" {
		return "", fmt.Errorf("%s.%s: must be a non-empty string", prefix, key)
	}

	return value, nil
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
