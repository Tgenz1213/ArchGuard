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

type Recorder interface {
	Record(event Event)
}

type Event interface {
	isEvent()
}

type RequestSent struct {
	Role  Role
	Usage inference.Usage
	Err   error
}

type CacheChecked struct {
	Role Role
	Hit  bool
}

type RetriesSent struct {
	Role    Role
	Retries int
}

type FileChecked struct {
	File  string
	Spent time.Duration
}

func (RequestSent) isEvent()  {}
func (CacheChecked) isEvent() {}
func (RetriesSent) isEvent()  {}
func (FileChecked) isEvent()  {}

type Fanout []Recorder

func (f Fanout) Record(event Event) {
	for _, recorder := range f {
		recorder.Record(event)
	}
}
