package stage

import "github.com/tgenz1213/archguard/internal/index"

type Threshold interface {
	For(adr *index.ADR) float64
}

type FixedMin float64

// ADRThreshold lets an ADR's own similarity_threshold override the global one.
type ADRThreshold struct{ Global float64 }

func (t ADRThreshold) For(adr *index.ADR) float64 { return index.EffectiveThreshold(adr, t.Global) }

func (m FixedMin) For(*index.ADR) float64 { return float64(m) }
