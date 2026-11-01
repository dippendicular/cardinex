package semantic

import "sort"

// Order selects the TopK ranking.
type Order int

const (
	ByCount Order = iota // Count.Upper
	BySum                // Sum.Upper
	ByMean               // Mean.Value
)

// Entity is one counter's view. Every interval comes from estimators.go.
type Entity struct {
	ID           uint64   `json:"id"`
	Count        Estimate `json:"count"`
	Sum          Estimate `json:"sum"`
	Mean         Estimate `json:"mean"`
	Ambiguous    float64  `json:"ambiguous"` // quarantined count attached to this counter, never folded in
	AmbiguousSum float64  `json:"ambiguous_sum"`
	Exemplars    []uint64 `json:"exemplars"` // distinct item IDs, by seeded priority
	Provisional  bool     `json:"provisional"`
	Aliases      []uint64 `json:"aliases,omitempty"`
}

func (s *Sketch) entity(c *counter) Entity {
	a, b := s.p.ValueMin, s.p.ValueMax
	ie := c.agg.ieff()
	ex := make([]uint64, len(c.ex))
	for i, e := range c.ex {
		ex[i] = e.item
	}
	al := append([]uint64(nil), c.id.aliases...)
	sort.Slice(al, func(i, j int) bool { return al[i] < al[j] })
	return Entity{
		ID:           c.id.id,
		Count:        CountInterval(c.agg.g, ie),
		Sum:          SumInterval(c.agg.G.sum, c.agg.inhSum, ie, b),
		Mean:         MeanInterval(c.agg.g, c.agg.G.sum, ie, a, b),
		Ambiguous:    float64(c.unc.count),
		AmbiguousSum: c.unc.sum.sum,
		Exemplars:    ex,
		Provisional:  c.id.provisional,
		Aliases:      al,
	}
}

func orderKey(e Entity, by Order) float64 {
	switch by {
	case BySum:
		return e.Sum.Upper
	case ByMean:
		return e.Mean.Value
	}
	return e.Count.Upper
}

// TopK returns up to k entities ranked by `by`, descending, ties to the lower
// ID. k <= 0 returns all.
func (s *Sketch) TopK(k int, by Order) []Entity {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entity, len(s.cs))
	for i, c := range s.cs {
		out[i] = s.entity(c)
	}
	sort.Slice(out, func(i, j int) bool {
		ki, kj := orderKey(out[i], by), orderKey(out[j], by)
		if ki != kj {
			return ki > kj
		}
		return out[i].ID < out[j].ID
	})
	if k > 0 && k < len(out) {
		out = out[:k]
	}
	return out
}

// Lookup returns the entity Add would confidently match vec to. It returns
// false for an ambiguous or unmatched vector, or an invalid one.
func (s *Sketch) Lookup(vec []float32) (Entity, bool) {
	code, err := s.enc.Encode(vec)
	if err != nil {
		return Entity{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hits, confident := s.classify(s.cs, code)
	if !confident {
		return Entity{}, false
	}
	return s.entity(hits[0].c), true
}

// Unmonitored returns the count and sum intervals that hold for any entity
// without a counter: [0, Ū] and [0, b·Ū].
func (s *Sketch) Unmonitored() (count, sum Estimate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.ubar()
	return UnmonitoredCount(u), UnmonitoredSum(u, s.p.ValueMax)
}

// EntityDelta is A − B for one entity. IDA/IDB are zero when absent.
type EntityDelta struct {
	IDA         uint64   `json:"id_a,omitempty"`
	IDB         uint64   `json:"id_b,omitempty"`
	InA         bool     `json:"in_a"`
	InB         bool     `json:"in_b"`
	Count       Estimate `json:"count"`
	Sum         Estimate `json:"sum"`
	Mean        Estimate `json:"mean"`
	Significant bool     `json:"significant"` // count delta interval excludes 0
}

// Diff matches s (A) against other (B) with the merge rule and returns A − B
// per entity: matched pairs, then A-only, then B-only, each in id order.
func (s *Sketch) Diff(other *Sketch) ([]EntityDelta, error) {
	if s == other {
		return nil, ErrSelfMerge
	}
	if s.p != other.p {
		return nil, ErrIncompatible
	}
	unlock := lockPair(s, other)
	defer unlock()

	a, b := s.p.ValueMin, s.p.ValueMax
	ubA, ubB := s.ubar(), other.ubar()
	missing := func(u uint64) Entity {
		return Entity{Count: UnmonitoredCount(u), Sum: UnmonitoredSum(u, b), Mean: UnmonitoredMean(a, b)}
	}
	delta := func(x, y Entity) EntityDelta {
		d := EntityDelta{Count: DiffInterval(x.Count, y.Count), Sum: DiffInterval(x.Sum, y.Sum), Mean: DiffInterval(x.Mean, y.Mean)}
		d.Significant = d.Count.Lower > 0 || d.Count.Upper < 0
		return d
	}

	as := sortedByID(s.cs)
	matchedB := map[*counter]bool{}
	var both, onlyA, onlyB []EntityDelta
	for _, ac := range as {
		hits, confident := other.classify(other.cs, ac.id.code)
		if confident {
			bc := hits[0].c
			matchedB[bc] = true
			d := delta(s.entity(ac), other.entity(bc))
			d.IDA, d.IDB, d.InA, d.InB = ac.id.id, bc.id.id, true, true
			both = append(both, d)
			continue
		}
		d := delta(s.entity(ac), missing(ubB))
		d.IDA, d.InA = ac.id.id, true
		onlyA = append(onlyA, d)
	}
	for _, bc := range sortedByID(other.cs) {
		if matchedB[bc] {
			continue
		}
		d := delta(missing(ubA), other.entity(bc))
		d.IDB, d.InB = bc.id.id, true
		onlyB = append(onlyB, d)
	}
	return append(append(both, onlyA...), onlyB...), nil
}

func sortedByID(cs []*counter) []*counter {
	out := append([]*counter(nil), cs...)
	sort.Slice(out, func(i, j int) bool { return out[i].id.id < out[j].id.id })
	return out
}
