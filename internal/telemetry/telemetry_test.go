package telemetry_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

// events is the test double a Recorder exists for: it sees every event without any report.
type events struct {
	mu  sync.Mutex
	log []string
}

func (e *events) add(format string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.log = append(e.log, fmt.Sprintf(format, args...))
}

func (e *events) RequestSent(role telemetry.Role, usage inference.Usage, err error) {
	e.add("request %s %d/%d reported=%v err=%v", role, usage.InputTokens, usage.OutputTokens, usage.Reported, err)
}

func (e *events) CacheChecked(role telemetry.Role, hit bool) { e.add("cache %s hit=%v", role, hit) }
func (e *events) RetriesSent(role telemetry.Role, n int)     { e.add("retries %s %d", role, n) }

func (e *events) FileChecked(file string, spent time.Duration) {
	e.add("file %s %v", file, spent)
}

func TestChatter_RecordsEveryRequestUnderItsRole(t *testing.T) {
	down := errors.New("down")
	fail := false
	provider := &inference.MockProvider{
		ChatUsage: inference.Usage{InputTokens: 10, OutputTokens: 2, Reported: true},
		ChatFunc: func(context.Context, string, string) (string, error) {
			if fail {
				return "", down
			}

			return "{}", nil
		},
	}
	recorded := &events{}
	chat := telemetry.NewChatter(provider, telemetry.Suggestion, recorded)

	if _, err := chat.Chat(t.Context(), "system", "user"); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	fail = true

	if _, err := chat.Chat(t.Context(), "system", "user"); !errors.Is(err, down) {
		t.Fatalf("Chat() error = %v, want the provider's", err)
	}

	want := []string{
		"request suggestion 10/2 reported=true err=<nil>",
		"request suggestion 0/0 reported=false err=down",
	}
	if !slices.Equal(recorded.log, want) {
		t.Errorf("events = %q, want %q", recorded.log, want)
	}
}

func TestEmbedder_RecordsEveryRequestAsAnEmbedding(t *testing.T) {
	recorded := &events{}
	embed := telemetry.NewEmbedder(&inference.MockProvider{EmbeddingDim: 2, EmbedUsage: inference.Usage{InputTokens: 4, Reported: true}}, recorded)

	if _, err := embed.CreateEmbedding(t.Context(), "text", inference.EmbeddingTaskQuery); err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}

	want := []string{"request embedding 4/0 reported=true err=<nil>"}
	if !slices.Equal(recorded.log, want) {
		t.Errorf("events = %q, want %q", recorded.log, want)
	}
}

func TestFanout_SendsEveryEventToEachRecorder(t *testing.T) {
	first, second := &events{}, &events{}
	fanout := telemetry.Fanout{first, second}

	fanout.RequestSent(telemetry.Judgment, inference.Usage{}, nil)
	fanout.CacheChecked(telemetry.Judgment, true)
	fanout.RetriesSent(telemetry.Judgment, 2)
	fanout.FileChecked("a.go", time.Second)

	if len(first.log) != 4 || !slices.Equal(first.log, second.log) {
		t.Errorf("first = %q, second = %q, want the same four events", first.log, second.log)
	}
}

func TestTally_Totals(t *testing.T) {
	reported := inference.Usage{InputTokens: 10, OutputTokens: 2, Reported: true}

	tests := []struct {
		name   string
		record func(*telemetry.Tally)
		want   telemetry.Totals
	}{
		{
			name:   "nothing recorded is a reported zero",
			record: func(*telemetry.Tally) {},
			want:   telemetry.Totals{Usage: inference.Usage{Reported: true}},
		},
		{
			name: "requests sum their usage",
			record: func(tally *telemetry.Tally) {
				tally.RequestSent(telemetry.Judgment, reported, nil)
				tally.RequestSent(telemetry.Judgment, reported, nil)
			},
			want: telemetry.Totals{Calls: 2, Usage: inference.Usage{InputTokens: 20, OutputTokens: 4, Reported: true}},
		},
		{
			name: "one unreported request makes the usage unreported",
			record: func(tally *telemetry.Tally) {
				tally.RequestSent(telemetry.Judgment, reported, nil)
				tally.RequestSent(telemetry.Judgment, inference.Usage{}, nil)
			},
			want: telemetry.Totals{Calls: 2, Usage: inference.Usage{InputTokens: 10, OutputTokens: 2}},
		},
		{
			name: "a failed request is a call with no tokens",
			record: func(tally *telemetry.Tally) {
				tally.RequestSent(telemetry.Judgment, inference.Usage{}, errors.New("down"))
			},
			want: telemetry.Totals{Calls: 1, Usage: inference.Usage{Reported: true}},
		},
		{
			name: "cache lookups and retries",
			record: func(tally *telemetry.Tally) {
				tally.CacheChecked(telemetry.Judgment, true)
				tally.CacheChecked(telemetry.Judgment, false)
				tally.RetriesSent(telemetry.Judgment, 2)
			},
			want: telemetry.Totals{CacheHits: 1, CacheMisses: 1, Retries: 2, Usage: inference.Usage{Reported: true}},
		},
		{
			name: "other roles are kept apart",
			record: func(tally *telemetry.Tally) {
				tally.RequestSent(telemetry.Suggestion, reported, nil)
				tally.CacheChecked(telemetry.Suggestion, true)
			},
			want: telemetry.Totals{Usage: inference.Usage{Reported: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tally := &telemetry.Tally{}
			tt.record(tally)

			if got := tally.Totals(telemetry.Judgment); got != tt.want {
				t.Errorf("Totals(judgment) = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTally_IsSafeForConcurrentRequests(t *testing.T) {
	tally := &telemetry.Tally{}

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { tally.RequestSent(telemetry.Judgment, inference.Usage{InputTokens: 10, Reported: true}, nil) })
	}

	wg.Wait()

	if got := tally.Totals(telemetry.Judgment); got.Calls != 20 || got.Usage.InputTokens != 200 {
		t.Errorf("Totals(judgment) = %+v, want 20 calls and 200 input tokens", got)
	}
}

func TestTally_FilesSlowestFirst(t *testing.T) {
	tally := &telemetry.Tally{}
	tally.FileChecked("b.go", time.Millisecond)
	tally.FileChecked("slow.go", time.Second)
	tally.FileChecked("a.go", time.Millisecond)

	want := []telemetry.FileTime{{File: "slow.go", Spent: time.Second}, {File: "a.go", Spent: time.Millisecond}, {File: "b.go", Spent: time.Millisecond}}
	if got := tally.Files(); !slices.Equal(got, want) {
		t.Errorf("Files() = %+v, want %+v", got, want)
	}
}
