package telemetry

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"github.com/tgenz1213/archguard/internal/inference"
)

type Totals struct {
	Calls       int
	CacheHits   int
	CacheMisses int
	Retries     int
	Usage       inference.Usage
}

type roleTotals struct {
	Totals
	unreported bool
}

type Tally struct {
	mu    sync.Mutex
	roles map[Role]*roleTotals
	files []FileChecked
}

func (t *Tally) Record(event Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch event := event.(type) {
	case RequestSent:
		t.addRequest(event)
	case CacheChecked:
		t.addCacheLookup(event)
	case RetriesSent:
		t.role(event.Role).Retries += event.Retries
	case FileChecked:
		t.files = append(t.files, event)
	}
}

func (t *Tally) addRequest(event RequestSent) {
	totals := t.role(event.Role)
	totals.Calls++

	if event.Err != nil {
		return
	}

	totals.Usage.InputTokens += event.Usage.InputTokens
	totals.Usage.OutputTokens += event.Usage.OutputTokens
	totals.unreported = totals.unreported || !event.Usage.Reported
}

func (t *Tally) addCacheLookup(event CacheChecked) {
	if event.Hit {
		t.role(event.Role).CacheHits++
	} else {
		t.role(event.Role).CacheMisses++
	}
}

func (t *Tally) role(role Role) *roleTotals {
	if t.roles == nil {
		t.roles = map[Role]*roleTotals{}
	}

	if t.roles[role] == nil {
		t.roles[role] = &roleTotals{}
	}

	return t.roles[role]
}

func (t *Tally) Totals(role Role) Totals {
	t.mu.Lock()
	defer t.mu.Unlock()

	totals := t.role(role)
	snapshot := totals.Totals
	snapshot.Usage.Reported = !totals.unreported

	return snapshot
}

func (t *Tally) Files() []FileChecked {
	t.mu.Lock()
	defer t.mu.Unlock()

	files := slices.Clone(t.files)
	slices.SortStableFunc(files, func(a, b FileChecked) int {
		if a.Spent != b.Spent {
			return cmp.Compare(b.Spent, a.Spent)
		}

		return strings.Compare(a.File, b.File)
	})

	return files
}
