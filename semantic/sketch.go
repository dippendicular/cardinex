// Package semantic implements the semantic aggregation sketch (model v0):
// per-entity count, sum and mean with guaranteed error intervals over
// embedding streams, where near-duplicate vectors denote one latent entity.
//
// The algorithm is specified in docs/SPEC.md and the interval formulas in
// docs/ESTIMATORS.md. Leader-style identity over frozen representatives is
// combined with weighted Space-Saving (Metwally et al. 2005) and the
// mergeable-summaries procedure (Agarwal et al., PODS 2012).
package semantic

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/dippendicular/cardinex/quant"
)

var (
	ErrValue        = errors.New("semantic: value is NaN or outside [value_min, value_max]")
	ErrIncompatible = errors.New("semantic: sketches have different parameters")
	ErrSelfMerge    = errors.New("semantic: cannot merge a sketch into itself")
	ErrBatchLength  = errors.New("semantic: batch slices have different lengths")
)

// BatchError reports the first failing index of an AddBatch call. The sketch
// is unchanged when it is returned.
type BatchError struct {
	Index int
	Err   error
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("semantic: batch item %d: %v", e.Index, e.Err)
}
func (e *BatchError) Unwrap() error { return e.Err }

// kahan is a compensated float64 sum.
type kahan struct{ sum, c float64 }

func (k *kahan) add(v float64) {
	y := v - k.c
	t := k.sum + y
	k.c = (t - k.sum) - y
	k.sum = t
}

func (k *kahan) addKahan(o kahan) {
	k.add(o.sum)
	k.add(-o.c)
}

// The three state categories (SPEC §4) are separate structs and never mixed.

type aggState struct {
	g        uint64
	G        kahan
	inh      uint64 // I, set to Ū at seeding
	floorAdd uint64 // f, floor contributions from merges
	inhSum   float64
}

func (a *aggState) ieff() uint64  { return a.inh + a.floorAdd }
func (a *aggState) upper() uint64 { return a.g + a.ieff() }

type member struct {
	code quant.Code
	item uint64
}

type idState struct {
	id          uint64
	code        quant.Code
	provisional bool
	confident   uint32
	aliases     []uint64
	members     []member
	seedItem    uint64
	seedValue   float64
	seedLive    bool // false once the seed has been re-anchored away
}

type uncState struct {
	count uint64
	sum   kahan
}

type exemplar struct{ prio, item uint64 }

type counter struct {
	agg aggState
	id  idState
	unc uncState
	ex  []exemplar // sorted by (prio, item), distinct items, len <= R
}

// Rejections counts rejected inputs by reason.
type Rejections struct {
	Dim, NonFinite, ZeroNorm, Value uint64
}

func (r *Rejections) add(o Rejections) {
	r.Dim += o.Dim
	r.NonFinite += o.NonFinite
	r.ZeroNorm += o.ZeroNorm
	r.Value += o.Value
}

// Stats is a snapshot of sketch-level state. See docs/SPEC.md §4 and §6.
type Stats struct {
	W        uint64  // confidently aggregated count
	WSum     float64 // confidently aggregated value
	Wq       uint64  // quarantined count, once per item
	WqSum    float64
	Orphaned uint64 // quarantined count whose counter was evicted, dropped or dissolved
	Floor    uint64 // F
	Ubar     uint64 // Ū
	Capacity int
	Live     int
	Rejected Rejections

	Promoted, ReAnchored, Dissolved uint64
	Fused, Aliased, Dropped         uint64
}

var instanceSeq atomic.Uint64

// Sketch is the semantic aggregation sketch. All methods are safe for
// concurrent use (SPEC §8).
type Sketch struct {
	mu  sync.Mutex
	seq uint64
	p   Params
	enc *quant.Encoder

	cs []*counter // live counters, in slot order

	floor    uint64
	w        uint64
	wSum     kahan
	wq       uint64
	wqSum    kahan
	orphaned uint64
	rejected Rejections

	promoted, reAnchored, dissolved uint64
	fused, aliased, dropped         uint64
}

// New builds an empty sketch.
func New(p Params) (*Sketch, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	enc, err := quant.NewEncoder(p.Dim, p.Bits, p.Seed)
	if err != nil {
		return nil, err
	}
	return &Sketch{seq: instanceSeq.Add(1), p: p, enc: enc}, nil
}

// Params returns the sketch parameters.
func (s *Sketch) Params() Params { return s.p }

// Encoder returns the sketch's encoder, so callers can reuse its codes.
func (s *Sketch) Encoder() *quant.Encoder { return s.enc }

// Add ingests one observation. It returns an error, and changes nothing but
// the rejection counts, if the vector or value is invalid (SPEC §3.1).
func (s *Sketch) Add(vec []float32, value float64, itemID uint64) error {
	code, err := s.prepare(vec, value)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.reject(err)
		return err
	}
	s.ingest(code, value, itemID)
	return nil
}

// AddBatch ingests observations in index order. It is all-or-nothing: every
// item is validated first, and on failure a *BatchError for the first
// failing index is returned and nothing is applied.
func (s *Sketch) AddBatch(vecs [][]float32, values []float64, ids []uint64) error {
	if len(vecs) != len(values) || len(vecs) != len(ids) {
		return ErrBatchLength
	}
	codes := make([]quant.Code, len(vecs))
	for i := range vecs {
		c, err := s.prepare(vecs[i], values[i])
		if err != nil {
			s.mu.Lock()
			s.reject(err)
			s.mu.Unlock()
			return &BatchError{Index: i, Err: err}
		}
		codes[i] = c
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range codes {
		s.ingest(codes[i], values[i], ids[i])
	}
	return nil
}

func (s *Sketch) prepare(vec []float32, value float64) (quant.Code, error) {
	if math.IsNaN(value) || value < s.p.ValueMin || value > s.p.ValueMax {
		return quant.Code{}, ErrValue
	}
	return s.enc.Encode(vec)
}

func (s *Sketch) reject(err error) {
	switch err {
	case quant.ErrDim:
		s.rejected.Dim++
	case quant.ErrNonFinite:
		s.rejected.NonFinite++
	case quant.ErrZeroNorm:
		s.rejected.ZeroNorm++
	case ErrValue:
		s.rejected.Value++
	}
}

type hit struct {
	c *counter
	d float64
}

// classify returns every counter within tau+band of code, sorted by
// (distance, id), and whether the match is confident (SPEC §5 steps 3-4).
func (s *Sketch) classify(list []*counter, code quant.Code) ([]hit, bool) {
	var hits []hit
	outer := s.p.Tau + s.p.Band
	for _, c := range list {
		if d := s.enc.Distance(code, c.id.code); d <= outer {
			hits = append(hits, hit{c, d})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].d != hits[j].d {
			return hits[i].d < hits[j].d
		}
		return hits[i].c.id.id < hits[j].c.id.id
	})
	return hits, len(hits) == 1 && hits[0].d <= s.p.Tau-s.p.Band
}

func (s *Sketch) ingest(code quant.Code, v float64, item uint64) {
	hits, confident := s.classify(s.cs, code)
	switch {
	case confident:
		c := hits[0].c
		c.agg.g++
		c.agg.G.add(v)
		c.id.confident++
		s.offerExemplar(c, item)
		s.w++
		s.wSum.add(v)
		if c.id.provisional {
			s.addMember(c, code, item)
			if len(c.id.members) == s.p.Probation {
				s.checkProbation(c)
			}
		}
	case len(hits) > 0:
		for _, h := range hits[:min(2, len(hits))] {
			h.c.unc.count++
			h.c.unc.sum.add(v)
		}
		s.wq++
		s.wqSum.add(v)
	default:
		s.seed(code, v, item)
	}
}

// ubar is Ū: max(F, min upper) when full, F otherwise.
func (s *Sketch) ubar() uint64 {
	if len(s.cs) < s.p.Capacity {
		return s.floor
	}
	m := s.cs[0].agg.upper()
	for _, c := range s.cs[1:] {
		if u := c.agg.upper(); u < m {
			m = u
		}
	}
	return max(s.floor, m)
}

func (s *Sketch) seed(code quant.Code, v float64, item uint64) {
	ub := s.ubar()
	var inhSum float64
	if len(s.cs) >= s.p.Capacity {
		vi := 0
		for i, c := range s.cs {
			vc, best := c.agg.upper(), s.cs[vi].agg.upper()
			if vc < best || (vc == best && c.id.id < s.cs[vi].id.id) {
				vi = i
			}
		}
		victim := s.cs[vi]
		inhSum = victim.agg.G.sum + victim.agg.inhSum
		s.orphaned += victim.unc.count
		s.cs = append(s.cs[:vi], s.cs[vi+1:]...)
	}
	c := &counter{}
	c.agg = aggState{g: 1, inh: ub, inhSum: inhSum}
	c.agg.G.add(v)
	c.id = idState{
		id:          quant.Mix64(s.p.Seed ^ quant.Mix64(item) ^ quant.Mix64(s.w+0x5851f42d4c957f2d)),
		code:        code,
		provisional: s.p.Probation > 0,
		confident:   1,
		seedItem:    item,
		seedValue:   v,
		seedLive:    true,
	}
	s.offerExemplar(c, item)
	s.cs = append(s.cs, c)
	s.w++
	s.wSum.add(v)
}

func (s *Sketch) exemplarPrio(item uint64) uint64 {
	return quant.Mix64(item ^ quant.Mix64(s.p.Seed^0x2545f4914f6cdd1d))
}

func (s *Sketch) offerExemplar(c *counter, item uint64) {
	if s.p.Exemplars == 0 {
		return
	}
	e := exemplar{s.exemplarPrio(item), item}
	i := sort.Search(len(c.ex), func(i int) bool { return !exLess(c.ex[i], e) })
	if i < len(c.ex) && c.ex[i] == e {
		return // distinct items only
	}
	if i >= s.p.Exemplars {
		return
	}
	c.ex = append(c.ex, exemplar{})
	copy(c.ex[i+1:], c.ex[i:])
	c.ex[i] = e
	if len(c.ex) > s.p.Exemplars {
		c.ex = c.ex[:s.p.Exemplars]
	}
}

func exLess(a, b exemplar) bool {
	if a.prio != b.prio {
		return a.prio < b.prio
	}
	return a.item < b.item
}

func (s *Sketch) removeExemplar(c *counter, item uint64) {
	for i, e := range c.ex {
		if e.item == item {
			c.ex = append(c.ex[:i], c.ex[i+1:]...)
			return
		}
	}
}

func (s *Sketch) addMember(c *counter, code quant.Code, item uint64) {
	if len(c.id.members) >= s.p.Probation || item == c.id.seedItem {
		return
	}
	for _, m := range c.id.members {
		if m.item == item {
			return
		}
	}
	c.id.members = append(c.id.members, member{code, item})
}

// checkProbation runs the promotion check (SPEC §5.1).
func (s *Sketch) checkProbation(c *counter) {
	ms := c.id.members
	mutual := true
	for i := 0; i < len(ms) && mutual; i++ {
		for j := i + 1; j < len(ms); j++ {
			if s.enc.Distance(ms[i].code, ms[j].code) > s.p.Tau {
				mutual = false
				break
			}
		}
	}
	if !mutual {
		s.dissolve(c)
		return
	}
	repOK := true
	for _, m := range ms {
		if s.enc.Distance(m.code, c.id.code) > s.p.Tau {
			repOK = false
			break
		}
	}
	if !repOK {
		c.id.code = ms[0].code
		if c.id.seedLive && c.agg.g > 0 {
			v := c.id.seedValue
			c.agg.g--
			c.agg.G.add(-v)
			c.unc.count++
			c.unc.sum.add(v)
			s.w--
			s.wSum.add(-v)
			s.wq++
			s.wqSum.add(v)
			s.removeExemplar(c, c.id.seedItem)
			c.id.seedLive = false
		}
		s.reAnchored++
	}
	c.id.provisional = false
	c.id.members = nil
	s.promoted++
}

func (s *Sketch) dissolve(c *counter) {
	s.floor = max(s.floor, s.ubar(), c.agg.upper())
	s.w -= c.agg.g
	s.wSum.add(-c.agg.G.sum)
	s.wq += c.agg.g
	s.wqSum.addKahan(c.agg.G)
	s.orphaned += c.agg.g + c.unc.count
	for i, x := range s.cs {
		if x == c {
			s.cs = append(s.cs[:i], s.cs[i+1:]...)
			break
		}
	}
	s.dissolved++
}

// Stats returns a snapshot of sketch-level state.
func (s *Sketch) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		W: s.w, WSum: s.wSum.sum, Wq: s.wq, WqSum: s.wqSum.sum, Orphaned: s.orphaned,
		Floor: s.floor, Ubar: s.ubar(), Capacity: s.p.Capacity, Live: len(s.cs), Rejected: s.rejected,
		Promoted: s.promoted, ReAnchored: s.reAnchored, Dissolved: s.dissolved,
		Fused: s.fused, Aliased: s.aliased, Dropped: s.dropped,
	}
}

func (c *counter) clone() *counter {
	n := *c
	n.id.code = c.id.code.Clone()
	n.id.aliases = append([]uint64(nil), c.id.aliases...)
	if c.id.members != nil {
		n.id.members = make([]member, len(c.id.members))
		for i, m := range c.id.members {
			n.id.members[i] = member{m.code.Clone(), m.item}
		}
	}
	n.ex = append([]exemplar(nil), c.ex...)
	return &n
}

// Clone returns a deep copy with a new identity for lock ordering.
func (s *Sketch) Clone() *Sketch {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := &Sketch{
		seq: instanceSeq.Add(1), p: s.p, enc: s.enc,
		floor: s.floor, w: s.w, wSum: s.wSum, wq: s.wq, wqSum: s.wqSum,
		orphaned: s.orphaned, rejected: s.rejected,
		promoted: s.promoted, reAnchored: s.reAnchored, dissolved: s.dissolved,
		fused: s.fused, aliased: s.aliased, dropped: s.dropped,
	}
	n.cs = make([]*counter, len(s.cs))
	for i, c := range s.cs {
		n.cs[i] = c.clone()
	}
	return n
}
