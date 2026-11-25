package telemetry_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

type events struct {
	mu  sync.Mutex
	log []telemetry.Event
}

func (e *events) Record(event telemetry.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.log = append(e.log, event)
}

func TestChatter_RecordsEveryRequestUnderItsRole(t *testing.T) {
	down := errors.New("down")
	fail := false
	usage := inference.Usage{InputTokens: 10, OutputTokens: 2, Reported: true}
	provider := &inference.MockProvider{
		ChatUsage: usage,
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

	want := []telemetry.Event{
		telemetry.RequestSent{Role: telemetry.Suggestion, Usage: usage},
		telemetry.RequestSent{Role: telemetry.Suggestion, Err: down},
	}
	if !slices.Equal(recorded.log, want) {
		t.Errorf("events = %+v, want %+v", recorded.log, want)
	}
}

func TestEmbedder_RecordsEveryRequestAsAnEmbedding(t *testing.T) {
	usage := inference.Usage{InputTokens: 4, Reported: true}
	recorded := &events{}
	embed := telemetry.NewEmbedder(&inference.MockProvider{EmbeddingDim: 2, EmbedUsage: usage}, recorded)

	if _, err := embed.CreateEmbedding(t.Context(), "text", inference.EmbeddingTaskQuery); err != nil {
		t.Fatalf("CreateEmbedding failed: %v", err)
	}

	want := []telemetry.Event{telemetry.RequestSent{Role: telemetry.Embedding, Usage: usage}}
	if !slices.Equal(recorded.log, want) {
		t.Errorf("events = %+v, want %+v", recorded.log, want)
	}
}

type askerFunc func() (inference.AskReply, error)

func (f askerFunc) Ask(context.Context, string, map[string]string) (inference.AskReply, error) {
	return f()
}

func TestAsker_RecordsEveryRequestAsAsking(t *testing.T) {
	down := errors.New("down")
	usage := inference.Usage{InputTokens: 30, OutputTokens: 1, Reported: true}
	replies := []error{nil, down}
	recorded := &events{}
	asker := telemetry.NewAsker(askerFunc(func() (inference.AskReply, error) {
		err := replies[0]
		replies = replies[1:]

		if err != nil {
			return inference.AskReply{}, err
		}

		return inference.AskReply{Answers: map[string]float64{"a": 0.5}, Usage: usage}, nil
	}), recorded)

	reply, err := asker.Ask(t.Context(), "code", map[string]string{"a": "q"})
	if err != nil || reply.Answers["a"] != 0.5 {
		t.Fatalf("Ask = %+v, %v; want the asker's reply", reply, err)
	}

	if _, err := asker.Ask(t.Context(), "code", map[string]string{"a": "q"}); !errors.Is(err, down) {
		t.Fatalf("Ask() error = %v, want the asker's", err)
	}

	want := []telemetry.Event{
		telemetry.RequestSent{Role: telemetry.Asking, Usage: usage},
		telemetry.RequestSent{Role: telemetry.Asking, Err: down},
	}
	if !slices.Equal(recorded.log, want) {
		t.Errorf("events = %+v, want %+v", recorded.log, want)
	}
}

func TestAsker_RecordsEachRetryAsARequest(t *testing.T) {
	usage := inference.Usage{InputTokens: 30, OutputTokens: 1, Reported: true}
	tally := &telemetry.Tally{}
	asker := telemetry.NewAsker(askerFunc(func() (inference.AskReply, error) {
		return inference.AskReply{Usage: usage, Retries: 2}, nil
	}), tally)

	if _, err := asker.Ask(t.Context(), "code", nil); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if got := tally.Totals(telemetry.Asking); got.Calls != 3 || got.Retries != 2 || got.Usage != usage {
		t.Errorf("asking totals = %+v, want 3 calls, 2 retries and the answer's usage", got)
	}
}

func TestFanout_SendsEveryEventToEachRecorder(t *testing.T) {
	first, second := &events{}, &events{}
	event := telemetry.FileChecked{File: "a.go", Spent: time.Second}

	telemetry.Fanout{first, second}.Record(event)

	want := []telemetry.Event{event}
	if !slices.Equal(first.log, want) || !slices.Equal(second.log, want) {
		t.Errorf("first = %+v, second = %+v, want %+v in each", first.log, second.log, want)
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
				tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: reported})
				tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: reported})
			},
			want: telemetry.Totals{Calls: 2, Usage: inference.Usage{InputTokens: 20, OutputTokens: 4, Reported: true}},
		},
		{
			name: "one unreported request makes the usage unreported",
			record: func(tally *telemetry.Tally) {
				tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: reported})
				tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: inference.Usage{}})
			},
			want: telemetry.Totals{Calls: 2, Usage: inference.Usage{InputTokens: 10, OutputTokens: 2}},
		},
		{
			name: "a failed request is a call with no tokens",
			record: func(tally *telemetry.Tally) {
				tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: inference.Usage{}, Err: errors.New("down")})
			},
			want: telemetry.Totals{Calls: 1, Usage: inference.Usage{Reported: true}},
		},
		{
			name: "cache lookups and retries",
			record: func(tally *telemetry.Tally) {
				tally.Record(telemetry.CacheChecked{Role: telemetry.Judgment, Hit: true})
				tally.Record(telemetry.CacheChecked{Role: telemetry.Judgment, Hit: false})
				tally.Record(telemetry.RetriesSent{Role: telemetry.Judgment, Retries: 2})
			},
			want: telemetry.Totals{CacheHits: 1, CacheMisses: 1, Retries: 2, Usage: inference.Usage{Reported: true}},
		},
		{
			name: "other roles are kept apart",
			record: func(tally *telemetry.Tally) {
				tally.Record(telemetry.RequestSent{Role: telemetry.Suggestion, Usage: reported})
				tally.Record(telemetry.CacheChecked{Role: telemetry.Suggestion, Hit: true})
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
		wg.Go(func() {
			tally.Record(telemetry.RequestSent{Role: telemetry.Judgment, Usage: inference.Usage{InputTokens: 10, Reported: true}})
		})
	}

	wg.Wait()

	if got := tally.Totals(telemetry.Judgment); got.Calls != 20 || got.Usage.InputTokens != 200 {
		t.Errorf("Totals(judgment) = %+v, want 20 calls and 200 input tokens", got)
	}
}

func TestTally_FilesSlowestFirst(t *testing.T) {
	tally := &telemetry.Tally{}
	tally.Record(telemetry.FileChecked{File: "b.go", Spent: time.Millisecond})
	tally.Record(telemetry.FileChecked{File: "slow.go", Spent: time.Second})
	tally.Record(telemetry.FileChecked{File: "a.go", Spent: time.Millisecond})

	want := []telemetry.FileChecked{{File: "slow.go", Spent: time.Second}, {File: "a.go", Spent: time.Millisecond}, {File: "b.go", Spent: time.Millisecond}}
	if got := tally.Files(); !slices.Equal(got, want) {
		t.Errorf("Files() = %+v, want %+v", got, want)
	}
}
