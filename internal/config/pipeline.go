package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	JobRank    = "rank"
	JobAsk     = "ask"
	JobJudge   = "judge"
	JobSuggest = "suggest"
)

const (
	RunnerCosine    = "cosine"
	RunnerSystemOne = "system-one"
	RunnerLLM       = "llm"
)

const retiredRunnerJev = "jev"

const (
	SystemOneAPIKeyEnv    = "ARCHGUARD_SYSTEM_ONE_API_KEY"
	SystemOneKeyOriginEnv = "ARCHGUARD_SYSTEM_ONE_KEY_ORIGIN"
)

const (
	DefaultSystemOneBaseURL = "https://api.typesafe.ai"
	DefaultSystemOneModel   = "jev-latest"
	DefaultSystemOneTimeout = 60 * time.Second
)

const systemOneReadme = `see "Asking System One models" in the README`

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
	BaseURL   string
	Model     string
	Timeout   time.Duration
	Question  Question
	LLM       LLMConfig
}

type Question struct {
	Type         string
	ForEach      string
	Instructions string
}

// ParsePipeline validates analysis.pipeline and the system_one block. LoadConfig never does, so commands other than check run on any pipeline.
func ParsePipeline(cfg *Config) ([]Node, error) {
	if isSet(&cfg.Jev) {
		return nil, fmt.Errorf("jev: the jev block is now system_one; rename it (%s)", systemOneReadme)
	}

	systemOne, err := parseSystemOne(&cfg.SystemOne)
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
		node, err := parseNode(nodeSource{cfg: cfg, systemOne: systemOne, position: i + 1, raw: resolveAlias(item)})
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

// ErrNeedsInit marks a config with no pipeline list, which archguard init can write.
var ErrNeedsInit = errors.New("run `archguard init` to write the default pipeline")

func needsInitError(retired bool) error {
	problem := "analysis.pipeline: missing"
	if retired {
		problem = "analysis.pipeline: the rank, rerank and pre_judge mapping is not a pipeline list"
	}

	indented := "  " + strings.ReplaceAll(strings.TrimSuffix(DefaultPipeline, "\n"), "\n", "\n  ")
	indented = strings.ReplaceAll(indented, "\n  \n", "\n\n")

	return fmt.Errorf("%s; %w, or add it under analysis:\n\nanalysis:\n%s", problem, ErrNeedsInit, indented)
}

func checkOrder(nodes []Node) error {
	judge := slices.IndexFunc(nodes, func(n Node) bool { return n.Job == JobJudge })
	judges := countJob(nodes, JobJudge)

	if judges != 1 {
		return fmt.Errorf("analysis.pipeline: needs exactly one judge node, found %d", judges)
	}

	if first := nodes[0]; !narrowsFirst(first) {
		return fmt.Errorf("analysis.pipeline: the first node, %q, must be a rank node, or an ask node with for_each: adr and a keep.top_k, so the candidates are narrowed before anything else asks about them", first.Name)
	}

	if suggests := countJob(nodes, JobSuggest); suggests > 1 {
		return fmt.Errorf("analysis.pipeline: needs at most one suggest node, found %d", suggests)
	}

	for _, after := range nodes[judge+1:] {
		if after.Job != JobSuggest {
			return fmt.Errorf("analysis.pipeline: node %q comes after the judge node; only a suggest node may follow the judge", after.Name)
		}
	}

	if suggest := slices.IndexFunc(nodes, func(n Node) bool { return n.Job == JobSuggest }); suggest >= 0 && suggest < judge {
		return fmt.Errorf("analysis.pipeline: suggest node %q comes before the judge node; it must follow the judge", nodes[suggest].Name)
	}

	return nil
}

func narrowsFirst(n Node) bool {
	return n.Job == JobRank || (n.Job == JobAsk && n.Question.ForEach == ForEachADR && n.TopK > 0)
}

func JudgeNode(nodes []Node) Node {
	return nodes[slices.IndexFunc(nodes, func(n Node) bool { return n.Job == JobJudge })]
}

// SuggestNode is the node whose LLM writes fixes: the suggest node, or the judge when there is none.
func SuggestNode(nodes []Node) Node {
	if i := slices.IndexFunc(nodes, func(n Node) bool { return n.Job == JobSuggest }); i >= 0 {
		return nodes[i]
	}

	return JudgeNode(nodes)
}

func countJob(nodes []Node, job string) int {
	count := 0
	for _, node := range nodes {
		if node.Job == job {
			count++
		}
	}

	return count
}

type systemOneSettings struct {
	baseURL string
	model   string
	timeout time.Duration
}

var systemOneKeys = []string{"base_url", "model", "timeout"}

func isSet(raw *yaml.Node) bool {
	raw = resolveAlias(raw)

	return raw.Kind != 0 && raw.Tag != "!!null"
}

func parseSystemOne(raw *yaml.Node) (systemOneSettings, error) {
	settings := systemOneSettings{baseURL: DefaultSystemOneBaseURL, model: DefaultSystemOneModel, timeout: DefaultSystemOneTimeout}
	if !isSet(raw) {
		return settings, nil
	}

	raw = resolveAlias(raw)
	if raw.Kind != yaml.MappingNode {
		return systemOneSettings{}, fmt.Errorf("system_one: must be a mapping with %s keys", strings.Join(systemOneKeys, ", "))
	}

	fields, err := decodeFields("system_one", raw)
	if err != nil {
		return systemOneSettings{}, err
	}

	if err := checkKeys("system_one", fields, systemOneKeys); err != nil {
		return systemOneSettings{}, err
	}

	return settings.override("system_one", fields)
}

func (s systemOneSettings) override(prefix string, fields map[string]yaml.Node) (systemOneSettings, error) {
	var err error

	if value, ok := fields["base_url"]; ok {
		if s.baseURL, err = decodeBaseURL(prefix, &value); err != nil {
			return systemOneSettings{}, err
		}
	}

	if value, ok := fields["model"]; ok {
		if s.model, err = decodeString(prefix, "model", &value); err != nil {
			return systemOneSettings{}, err
		}
	}

	if value, ok := fields["timeout"]; ok {
		if s.timeout, err = decodeTimeout(prefix, &value); err != nil {
			return systemOneSettings{}, err
		}
	}

	return s, nil
}

func decodeBaseURL(prefix string, node *yaml.Node) (string, error) {
	raw, err := decodeString(prefix, "base_url", node)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Opaque != "" {
		return "", fmt.Errorf("%s.base_url: %q must be an http or https URL with a host", prefix, raw)
	}

	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") {
		return "", fmt.Errorf("%s.base_url: %q can't have a user, query or fragment", prefix, raw)
	}

	return strings.TrimRight(raw, "/"), nil
}

func decodeTimeout(prefix string, node *yaml.Node) (time.Duration, error) {
	node = resolveAlias(node)

	timeout, err := time.ParseDuration(node.Value)
	if node.Tag != "!!str" || err != nil || timeout <= 0 {
		return 0, fmt.Errorf("%s.timeout: %q must be a positive duration such as 60s or 2m", prefix, node.Value)
	}

	return timeout, nil
}
