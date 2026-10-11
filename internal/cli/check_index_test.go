package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/tgenz1213/archguard/internal/index"
	"github.com/tgenz1213/archguard/internal/output"
)

func TestStaleReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "missing", err: fmt.Errorf("%w: .archguard/index.json", index.ErrIndexMissing), want: "index missing"},
		{name: "ADRs changed", err: fmt.Errorf("%w: saved hash a, current b", index.ErrADRsChanged), want: "ADRs changed"},
		{
			name: "model and dimension",
			err:  errors.Join(fmt.Errorf("%w: model", index.ErrEmbeddingModelChanged), fmt.Errorf("%w: dim", index.ErrEmbeddingModelChanged)),
			want: "embedding model changed",
		},
		{name: "embedding format", err: fmt.Errorf("%w: saved \"\", current \"1\"", index.ErrEmbeddingFormatChanged), want: "embedding format changed"},
		{name: "unreadable", err: errors.New("invalid character"), want: "saved index unreadable"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := staleReason(tc.err); got != tc.want {
				t.Errorf("staleReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnindexedADRs(t *testing.T) {
	store := index.NewLocalStore(1)
	store.ADRs = []index.ADR{{RelPath: "0001-indexed.md"}}

	fetched := []index.ADR{{RelPath: "0001-indexed.md"}, {RelPath: "0002-failed.md"}, {RelPath: "0003-absent.md"}}
	skipped := []index.SkippedADR{{RelPath: "0002-failed.md", Err: errors.New("too long")}}

	run := &checkRun{log: output.Discard()}
	got := run.unindexedADRs(store, fetched, skipped)

	if len(got) != 2 || got[0].ADR != "0002-failed.md" || got[1].ADR != "0003-absent.md" {
		t.Fatalf("got %+v, want the two ADRs missing from the index", got)
	}

	if got[0].Reason == got[1].Reason {
		t.Errorf("an ADR that failed to embed this run should carry its error, got %+v", got)
	}
}
