package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/analysis/stage"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestExitCodeForAnalysisError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ExitCode
	}{
		{"direct drift detection error", &analysis.DriftDetectedError{Count: 2}, ExitDriftDetected},
		{"wrapped drift detection error", fmt.Errorf("wrapped: %w", &analysis.DriftDetectedError{Count: 2}), ExitDriftDetected},
		{"operational error", errors.New("git content provider failure"), ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCodeForAnalysisError(tt.err); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestStageFailureExit(t *testing.T) {
	unavailable := analysis.StageFailure{Stage: "rank", File: "a.go", Kind: stage.KindUnavailable}
	precondition := analysis.StageFailure{Stage: "rank", File: "b.go", Kind: stage.KindPreconditionNotMet}
	tests := []struct {
		name     string
		failures []analysis.StageFailure
		want     ExitCode
	}{
		{"none", nil, ExitSuccess},
		{"unavailable only", []analysis.StageFailure{unavailable}, ExitStageUnavailable},
		{"precondition only", []analysis.StageFailure{precondition}, ExitStagePrecondition},
		{"both kinds", []analysis.StageFailure{unavailable, precondition, unavailable}, ExitStagePrecondition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := stageFailureExit(tt.failures)
			if code != tt.want || (err != nil) != (tt.want != ExitSuccess) {
				t.Fatalf("stageFailureExit = (%d, %v), want code %d", code, err, tt.want)
			}
		})
	}
}

func TestWriteCheckReport_FailuresOmittedWhenNone(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCheckReport(&buf, &analysis.Engine{}, nil); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(buf.String(), "failures") {
		t.Errorf("report %q should not mention failures when there are none", buf.String())
	}
}

func TestWriteCheckReport_IncludesFailureKind(t *testing.T) {
	var buf bytes.Buffer

	failures := []analysis.StageFailure{{Stage: "rank", File: "a.go", Kind: stage.KindUnavailable, Error: "down"}}
	if err := writeCheckReport(&buf, &analysis.Engine{StageFailures: failures}, nil); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Failures []map[string]string `json:"failures"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}

	if len(got.Failures) != 1 || got.Failures[0]["kind"] != "unavailable" || got.Failures[0]["node"] != "rank" || got.Failures[0]["file"] != "a.go" || got.Failures[0]["error"] != "down" {
		t.Errorf("failures = %v", got.Failures)
	}
}

func TestResolveContentProvider(t *testing.T) {
	tests := []struct {
		name           string
		files          []string
		staged         bool
		all            bool
		updateBaseline bool
		since          string
		want           analysis.ContentProvider
	}{
		{
			name: "no args or flags defaults to uncommitted",
			want: &analysis.UncommittedProvider{},
		},
		{
			name:  "dot positional arg scans everything",
			files: []string{"."},
			want:  &analysis.AllProvider{},
		},
		{
			name:  "specific file arg scans just that file",
			files: []string{"internal/foo.go"},
			want:  &analysis.MultiFileProvider{Paths: []string{"internal/foo.go"}},
		},
		{
			name:  "multiple file args scan all of them",
			files: []string{"internal/foo.go", "internal/bar.go"},
			want:  &analysis.MultiFileProvider{Paths: []string{"internal/foo.go", "internal/bar.go"}},
		},
		{
			name:  "dot mixed with other file args still scans everything",
			files: []string{".", "internal/foo.go"},
			want:  &analysis.AllProvider{},
		},
		{
			name:  "dot as a non-first arg still scans everything",
			files: []string{"internal/foo.go", "."},
			want:  &analysis.AllProvider{},
		},
		{
			name:   "staged flag scans staged files",
			staged: true,
			want:   &analysis.StagedProvider{},
		},
		{
			name: "all flag scans all tracked files",
			all:  true,
			want: &analysis.AllProvider{},
		},
		{
			name:           "update-baseline alone scans everything",
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:           "update-baseline overrides staged",
			staged:         true,
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:           "update-baseline overrides a file arg",
			files:          []string{"internal/foo.go"},
			updateBaseline: true,
			want:           &analysis.AllProvider{},
		},
		{
			name:  "since scans the files changed since that ref",
			since: "abc123",
			want:  &analysis.ChangedSinceProvider{Ref: "abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkCmd{Paths: tt.files, Staged: tt.staged, All: tt.all, UpdateBaseline: tt.updateBaseline, Since: baseRef(tt.since)}.contentProvider(output.New(os.Stdout, false))
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tt.want) {
				t.Fatalf("expected type %T, got %T", tt.want, got)
			}

			if mfp, ok := got.(*analysis.MultiFileProvider); ok {
				wantMFP := tt.want.(*analysis.MultiFileProvider)
				if !slices.Equal(mfp.Paths, wantMFP.Paths) {
					t.Errorf("expected paths %v, got %v", wantMFP.Paths, mfp.Paths)
				}
			}
		})
	}
}

func TestResolveContentProvider_DotMixedWithExtraArgsWarns(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{name: "dot first", files: []string{".", "internal/foo.go"}},
		{name: "dot not first", files: []string{"internal/foo.go", "."}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got analysis.ContentProvider
			captured := captureStdout(t, func() {
				got = checkCmd{Paths: tt.files}.contentProvider(output.New(os.Stdout, false))
			})

			if _, ok := got.(*analysis.AllProvider); !ok {
				t.Fatalf("expected *analysis.AllProvider, got %T", got)
			}

			if !strings.Contains(captured, "internal/foo.go") {
				t.Errorf("expected a warning naming the ignored extra argument %q, got output: %q", "internal/foo.go", captured)
			}
		})
	}
}
