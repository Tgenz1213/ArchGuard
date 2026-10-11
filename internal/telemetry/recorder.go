package telemetry

import (
	"time"

	"github.com/tgenz1213/archguard/internal/inference"
)

type Role string

const (
	Judgment   Role = "judgment"
	Suggestion Role = "suggestion"
	Embedding  Role = "embedding"
)

// Recorder is told what a run did, where it happens. Implementations must be safe for concurrent use
// and must never write to stdout, which carries the report (docs/arch/0035-run-recorder-seam.md).
type Recorder interface {
	// A failed request is still sent; its usage is the zero value.
	RequestSent(role Role, usage inference.Usage, err error)
	CacheChecked(role Role, hit bool)
	RetriesSent(role Role, retries int)
	FileChecked(file string, spent time.Duration)
}

type Nop struct{}

func (Nop) RequestSent(Role, inference.Usage, error) {}
func (Nop) CacheChecked(Role, bool)                  {}
func (Nop) RetriesSent(Role, int)                    {}
func (Nop) FileChecked(string, time.Duration)        {}

type Fanout []Recorder

func (f Fanout) RequestSent(role Role, usage inference.Usage, err error) {
	for _, recorder := range f {
		recorder.RequestSent(role, usage, err)
	}
}

func (f Fanout) CacheChecked(role Role, hit bool) {
	for _, recorder := range f {
		recorder.CacheChecked(role, hit)
	}
}

func (f Fanout) RetriesSent(role Role, retries int) {
	for _, recorder := range f {
		recorder.RetriesSent(role, retries)
	}
}

func (f Fanout) FileChecked(file string, spent time.Duration) {
	for _, recorder := range f {
		recorder.FileChecked(file, spent)
	}
}
