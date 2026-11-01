package semantic

import "github.com/dippendicular/cardinex/core/quant"

// index finds the counters whose representative lies within radius of a code
// (SPEC §5 step 2). Results may be in any order; classify sorts them.
type index interface {
	near(list []*counter, code quant.Code, radius float64) []hit
}

// linearIndex scans every counter. It is exact and fine up to capacity 16384;
// a sublinear index can replace it behind the same interface.
type linearIndex struct{ m quant.Metric }

func (l linearIndex) near(list []*counter, code quant.Code, radius float64) []hit {
	var hits []hit
	for _, c := range list {
		if d := l.m.Distance(code, c.id.code); d <= radius {
			hits = append(hits, hit{c, d})
		}
	}
	return hits
}

func (s *Sketch) index() index { return linearIndex{s.enc} }
