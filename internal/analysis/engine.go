package analysis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/baseline"
	"github.com/tgenz1213/archguard/internal/cache"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/output"
	"golang.org/x/sync/errgroup"
)

type Engine struct {
	Config               *config.Config
	Store                index.VectorStore
	Chat                 inference.Chatter
	Embed                inference.Embedder
	Content              ContentProvider
	Debug                bool
	CI                   bool
	Cache                *cache.Cache
	Baseline             *baseline.Baseline
	UpdateBaseline       bool
	BaselineReason       string
	CollectedBaseline    *baseline.Baseline
	SkippedFiles         []output.FileGap
	FailedChecks         []output.FailedCheck
	Baselined            int
	RecordedEntries      []output.RecordedEntry
	UnrecordedViolations []output.UnrecordedViolation
	PartialFiles         []output.FileGap
	JSONOutput           bool
	Out                  *output.Printer
	CollectedViolations  []Violation
	CollectedStages      []stage.Stats
	StageFailures        []StageFailure
	// Off by default: adds one LLM call per reported violation.
	SuggestFixes bool
	Stages       []stage.Stage
}

type Violation struct {
	File       string `json:"file"`
	ADRID      string `json:"adr_id"`
	ADRTitle   string `json:"adr_title"`
	Line       int    `json:"line"`
	Reasoning  string `json:"reasoning"`
	QuotedCode string `json:"quoted_code"`
	Suggestion string `json:"suggestion,omitempty"`
	// Line counts lines of the diff the LLM saw, not of the file.
	lineInDiff bool
	unverified bool
}

func (v Violation) Annotation() output.Annotation {
	a := output.Annotation{File: v.File, ADRID: v.ADRID, ADRTitle: v.ADRTitle, Message: v.Reasoning}
	if !v.lineInDiff {
		a.Line = v.Line
	}

	return a
}

type StageFailure struct {
	Stage string     `json:"stage"`
	File  string     `json:"file"`
	Kind  stage.Kind `json:"kind"`
	Error string     `json:"error"`
}

var ErrDriftDetected = errors.New("architectural drift detected")

type DriftDetectedError struct {
	Count int
}

func (e *DriftDetectedError) Error() string {
	return fmt.Sprintf("found %d architectural violations", e.Count)
}

func (e *DriftDetectedError) Is(target error) bool {
	return target == ErrDriftDetected
}

func NewEngine(cfg *config.Config, store index.VectorStore, chat inference.Chatter, embed inference.Embedder, content ContentProvider) *Engine {
	return &Engine{
		Config:  cfg,
		Store:   store,
		Chat:    chat,
		Embed:   embed,
		Content: content,
	}
}

func (e *Engine) printer() *output.Printer {
	if e.Out != nil {
		return e.Out
	}

	return output.New(nil, e.Debug)
}

func (e *Engine) Run(ctx context.Context) error {
	run := newRunState(e.printer(), e.scoringStages())

	files, err := e.Content.GetFiles(ctx)
	if err != nil {
		e.collectStageStats(run)
		return err
	}

	if err := e.checkFiles(ctx, run, files); err != nil {
		return err
	}

	// Checked before any summary or baseline snapshot: a partial scan must never look complete.
	if err := ctx.Err(); err != nil {
		return err
	}

	e.publish(run)

	if !e.UpdateBaseline && run.violations > 0 {
		return &DriftDetectedError{Count: run.violations}
	}

	return nil
}

func (e *Engine) scoringStages() []stage.Stage {
	if len(e.Stages) > 0 {
		return e.Stages
	}

	return []stage.Stage{stage.NewCosineStage(e.Store, e.Embed, e.Config.VectorStore.SimilarityThreshold, e.Config.Analysis.RelevantADRLimit())}
}

func (e *Engine) maxConcurrency() int {
	if e.Config.Analysis.MaxConcurrency <= 0 {
		return 5
	}

	return e.Config.Analysis.MaxConcurrency
}

func (e *Engine) checkFiles(ctx context.Context, run *runState, files []string) error {
	var group errgroup.Group
	group.SetLimit(e.maxConcurrency())

	_, explicitFiles := e.Content.(*MultiFileProvider)

	for _, file := range files {
		if e.shouldExclude(file) {
			if explicitFiles && file != baseline.Path {
				run.out.Debug("Skipping %s: explicitly requested but matches exclude_patterns", file)
			}

			continue
		}

		group.Go(func() error {
			if ctx.Err() == nil {
				e.checkFile(ctx, run, file)
			}

			return nil
		})
	}

	return group.Wait()
}

func (e *Engine) collectStageStats(run *runState) {
	if e.JSONOutput {
		e.CollectedStages = run.telemetry.Stats()
	}
}

func (e *Engine) publish(run *runState) {
	e.SkippedFiles = run.skipped
	e.FailedChecks = run.failedChecks
	e.Baselined = run.baselined
	e.RecordedEntries = run.recorded
	e.UnrecordedViolations = run.unrecorded
	e.PartialFiles = run.partial
	e.StageFailures = sortedStageFailures(run.stageFailures)
	e.CollectedViolations = run.collected
	e.collectStageStats(run)

	if e.UpdateBaseline {
		snapshot := baseline.New()
		for _, entry := range run.entries {
			snapshot.Add(entry)
		}

		e.CollectedBaseline = snapshot
	}
}

func sortedStageFailures(failures []StageFailure) []StageFailure {
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].File != failures[j].File {
			return failures[i].File < failures[j].File
		}

		return failures[i].Stage < failures[j].Stage
	})

	return failures
}

func (e *Engine) shouldExclude(path string) bool {
	// Always excluded, not conditional on exclude_patterns: the baseline
	// file quotes prior violations and must never be scanned as source.
	if path == baseline.Path {
		return true
	}

	for _, pattern := range e.Config.Analysis.ExcludePatterns {
		if index.MatchGlob(pattern, path) {
			return true
		}
	}

	return false
}

// fetchContext also returns the untruncated content so callers needing both don't re-read the file.
func (e *Engine) fetchContext(ctx context.Context, path string) (content, fullContent, mode string, err error) {
	maxTokens := e.Config.LLM.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8000
	}

	fullContent, err = e.Content.GetContent(ctx, path)
	if err != nil {
		return "", "", "", err
	}

	totalTokens, err := e.Chat.CountTokens(ctx, fullContent)
	if err != nil {
		return "", "", "", fmt.Errorf("counting tokens for %s: %w", path, err)
	}

	if totalTokens <= maxTokens {
		return fullContent, fullContent, "full", nil
	}

	// A diff only covers the uncommitted-vs-HEAD hunk, which can't satisfy
	// --update-baseline's whole-file snapshot contract (docs/arch/0006).
	if !e.UpdateBaseline {
		diff, err := e.Content.GetDiff(ctx, path)
		if err == nil && diff != "" {
			return diff, fullContent, "diff", nil
		}
	}

	truncated, err := e.truncateToTokenLimit(ctx, fullContent, totalTokens, maxTokens)
	if err != nil {
		return "", "", "", fmt.Errorf("truncating content for %s: %w", path, err)
	}

	return truncated, fullContent, "truncated", nil
}

func (e *Engine) truncateToTokenLimit(ctx context.Context, content string, totalTokens, maxTokens int) (string, error) {
	bytesPerToken := float64(len(content)) / float64(totalTokens)
	cut := clampRuneBoundary(content, int(float64(maxTokens)*bytesPerToken))
	candidate := content[:cut]

	const maxProportionalAttempts = 5
	fits := false
	for attempt := 0; attempt < maxProportionalAttempts && cut > 0; attempt++ {
		n, err := e.Chat.CountTokens(ctx, candidate)
		if err != nil {
			return "", err
		}

		if n <= maxTokens {
			fits = true
			break
		}

		cut = clampRuneBoundary(content, int(float64(cut)*float64(maxTokens)/float64(n)))
		candidate = content[:cut]
	}

	for !fits && cut > 0 {
		// Halve before measuring, not after: the entry candidate is already
		// known to exceed maxTokens, so re-measuring it would be redundant.
		cut = clampRuneBoundary(content, cut/2)
		candidate = content[:cut]

		n, err := e.Chat.CountTokens(ctx, candidate)
		if err != nil {
			return "", err
		}

		if n <= maxTokens {
			fits = true
		}
	}

	if lastNewline := strings.LastIndex(candidate, "\n"); lastNewline != -1 {
		candidate = candidate[:lastNewline+1]
	}

	return candidate, nil
}

func clampRuneBoundary(s string, cut int) int {
	if cut < 0 {
		return 0
	}

	if cut >= len(s) {
		return len(s)
	}

	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return cut
}

func truncateRuneSafe(s string, limit int) string {
	return s[:clampRuneBoundary(s, limit)]
}

func rollBackToNewline(s string) string {
	if lastNewline := strings.LastIndex(s, "\n"); lastNewline != -1 {
		return s[:lastNewline+1]
	}

	return s
}

// Patch syntax would skew the embedding away from code-vs-ADR-prose similarity.
func stripDiffMetadata(s string) string {
	if !isUnifiedDiff(s) {
		return s
	}

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	inHunk := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case !inHunk:
		case strings.HasPrefix(line, "diff --git "):
			// a second file's preamble in (unsupported) multi-file input
			inHunk = false
		case strings.HasPrefix(line, "\\"):
			// git's "\ No newline at end of file" marker
		default:
			if line != "" {
				out = append(out, line[1:])
			} else {
				out = append(out, "")
			}
		}
	}

	return strings.Join(out, "\n")
}

var hunkHeaderPattern = regexp.MustCompile(`(?m)^@@ -\d+(?:,\d+)? \+\d+(?:,\d+)? @@`)

// Requires a git header too, so a doc containing an example "@@" hunk isn't mistaken for a diff.
func isUnifiedDiff(s string) bool {
	hasGitHeader := strings.Contains("\n"+s, "\ndiff --git ")
	return hasGitHeader && hunkHeaderPattern.MatchString(s)
}

func (e *Engine) findLineNumber(content, quote string) int {
	if quote == "" {
		return 0
	}

	idx := strings.Index(content, quote)
	if idx == -1 {
		return 0
	}

	lines := strings.Split(content[:idx], "\n")
	return len(lines)
}

func newStageFailure(name, file string, err error) StageFailure {
	failure := StageFailure{Stage: name, File: file, Error: err.Error()}

	var stageErr *stage.Error
	if errors.As(err, &stageErr) {
		failure.Kind = stageErr.Kind
	}

	return failure
}

func failureMessage(f StageFailure) string {
	return fmt.Sprintf("stage %s failed (%s): %s", f.Stage, f.Kind, f.Error)
}

func scoringErrorMessage(err error) string {
	var stageErr *stage.Error
	if errors.As(err, &stageErr) {
		return fmt.Sprintf("%s: %v", stageErr.Action, stageErr.Err)
	}

	return fmt.Sprintf("scoring candidates: %v", err)
}
