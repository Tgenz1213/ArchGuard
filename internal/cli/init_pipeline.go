package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/tgenz1213/archguard/internal/config"
	"gopkg.in/yaml.v3"
)

const defaultIndent = 2

// pipelineSplice says what writing the default pipeline into a config would do.
type pipelineSplice struct {
	result      []byte
	alreadyList bool
	hadPreJudge bool
}

// spliceDefaultPipeline writes the default pipeline into a config's text in place of analysis.pipeline, leaving every other line as it was.
func spliceDefaultPipeline(content []byte) (pipelineSplice, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return pipelineSplice{}, fmt.Errorf("failed to parse config: %w", err)
	}

	analysisKey, analysis := mappingEntry(documentRoot(&doc), "analysis")
	pipelineKey, pipeline := mappingEntry(analysis, "pipeline")

	if pipeline != nil && pipeline.Kind == yaml.SequenceNode {
		return pipelineSplice{alreadyList: true}, nil
	}

	if err := checkSpliceable(analysis, pipeline); err != nil {
		return pipelineSplice{}, err
	}

	text := configText{lines: splitLines(content), newline: newlineOf(content)}
	splice := pipelineSplice{hadPreJudge: hasPreJudge(pipeline)}

	switch {
	case pipelineKey != nil:
		text.replaceBlock(pipelineKey.Line, pipelineKey.Column-1, defaultPipelineLines(pipelineKey.Column-1, indentUnit(analysis)))
	case analysisKey != nil:
		end := text.blockEnd(analysisKey.Line, analysisKey.Column-1)
		text.insertAfter(end, defaultPipelineLines(indentUnit(analysis), indentUnit(analysis)))
	default:
		text.insertAfter(len(text.lines), append([]string{"analysis:"}, defaultPipelineLines(defaultIndent, defaultIndent)...))
	}

	splice.result = text.bytes()

	return splice, nil
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}

	return nil
}

func mappingEntry(mapping *yaml.Node, key string) (keyNode, value *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, nil
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}

	return nil, nil
}

var errNotSpliceable = errors.New("analysis is written in a way init can't edit safely (flow style, an anchor, an alias or a merge key); replace analysis.pipeline by hand with the default pipeline from the README")

// Line-based editing is only safe when analysis and its pipeline are plain block YAML that nothing else refers to.
func checkSpliceable(analysis, pipeline *yaml.Node) error {
	for _, node := range []*yaml.Node{analysis, pipeline} {
		if node != nil && (node.Kind == yaml.AliasNode || node.Anchor != "" || node.Style&yaml.FlowStyle != 0) {
			return errNotSpliceable
		}
	}

	if mergeKey, _ := mappingEntry(analysis, "<<"); mergeKey != nil {
		return errNotSpliceable
	}

	return nil
}

func hasPreJudge(pipeline *yaml.Node) bool {
	key, _ := mappingEntry(pipeline, "pre_judge")
	return key != nil
}

// indentUnit is how far analysis indents its keys, so the pipeline matches the file.
func indentUnit(analysis *yaml.Node) int {
	if analysis == nil || analysis.Kind != yaml.MappingNode || len(analysis.Content) == 0 {
		return defaultIndent
	}

	return analysis.Content[0].Column - 1
}

// defaultPipelineLines indents config.DefaultPipeline to start at column base and nest the list by unit; a list item's keys stay two past its "- ".
func defaultPipelineLines(base, unit int) []string {
	source := strings.Split(strings.TrimSuffix(config.DefaultPipeline, "\n"), "\n")
	lines := make([]string, len(source))

	for i, line := range source {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			continue
		}

		indent := base
		if depth := (len(line) - len(trimmed)) / defaultIndent; depth > 0 {
			indent += unit + (depth-1)*defaultIndent
		}

		lines[i] = strings.Repeat(" ", indent) + trimmed
	}

	return lines
}

type configText struct {
	lines   []string
	newline string
}

func splitLines(content []byte) []string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if text == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func newlineOf(content []byte) string {
	if bytes.Contains(content, []byte("\r\n")) {
		return "\r\n"
	}

	return "\n"
}

// blockEnd returns the last line, 1-based, of the block whose key is on line start at indent: the last line after it indented deeper, blank lines aside.
func (t *configText) blockEnd(start, indent int) int {
	end := start

	for i := start; i < len(t.lines); i++ {
		line := t.lines[i]
		trimmed := strings.TrimLeft(line, " ")

		if trimmed == "" {
			continue
		}

		if len(line)-len(trimmed) <= indent {
			break
		}

		end = i + 1
	}

	return end
}

func (t *configText) replaceBlock(start, indent int, replacement []string) {
	end := t.blockEnd(start, indent)
	t.lines = append(t.lines[:start-1], append(replacement, t.lines[end:]...)...)
}

func (t *configText) insertAfter(line int, inserted []string) {
	t.lines = append(t.lines[:line], append(inserted, t.lines[line:]...)...)
}

func (t *configText) bytes() []byte {
	return []byte(strings.Join(t.lines, t.newline) + t.newline)
}
