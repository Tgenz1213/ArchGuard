package telemetry

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tgenz1213/archguard/internal/inference"
)

// Usage.Reported is false when any request in the total was unreported, since a partial sum would undercount.
type Totals struct {
	Calls       int
	CacheHits   int
	CacheMisses int
	Retries     int
	Usage       inference.Usage
}

type FileTime struct {
	File  string
	Spent time.Duration
}

type roleTotals struct {
	Totals
	unreported bool
}

// Tally keeps a run's totals in memory; the JSON usage is built from it. The zero value is ready to use.
type Tally struct {
	mu    sync.Mutex
	roles map[Role]*roleTotals
	files []FileTime
}

func (t *Tally) RequestSent(role Role, usage inference.Usage, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	totals := t.role(role)
	totals.Calls++

	if err != nil {
		return
	}

	totals.Usage.InputTokens += usage.InputTokens
	totals.Usage.OutputTokens += usage.OutputTokens
	totals.unreported = totals.unreported || !usage.Reported
}

func (t *Tally) CacheChecked(role Role, hit bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if hit {
		t.role(role).CacheHits++
	} else {
		t.role(role).CacheMisses++
	}
}

func (t *Tally) RetriesSent(role Role, retries int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.role(role).Retries += retries
}

func (t *Tally) FileChecked(file string, spent time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.files = append(t.files, FileTime{File: file, Spent: spent})
}

// Callers hold mu.
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

// Files lists every checked file, slowest first.
func (t *Tally) Files() []FileTime {
	t.mu.Lock()
	defer t.mu.Unlock()

	files := slices.Clone(t.files)
	slices.SortStableFunc(files, func(a, b FileTime) int {
		if a.Spent != b.Spent {
			return cmp.Compare(b.Spent, a.Spent)
		}

		return strings.Compare(a.File, b.File)
	})

	return files
}
