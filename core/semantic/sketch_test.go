package semantic

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/dippendicular/cardinex/core/quant"
)

func basis(dim, i int) []float32 {
	v := make([]float32, dim)
	v[i] = 1
	return v
}

// plane returns cos(phi)·e0 + sin(phi)·e1, so angle(plane(x), plane(y)) = |x−y|.
func plane(dim int, phi float64) []float32 {
	v := make([]float32, dim)
	v[0], v[1] = float32(math.Cos(phi)), float32(math.Sin(phi))
	return v
}

func mustNew(t *testing.T, p Params) *Sketch {
	t.Helper()
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustAdd(t *testing.T, s *Sketch, v []float32, val float64, id uint64) {
	t.Helper()
	if err := s.Add(v, val, id); err != nil {
		t.Fatal(err)
	}
}

func TestParamsValidation(t *testing.T) {
	bad := []func(*Params){
		func(p *Params) { p.Dim = 0 },
		func(p *Params) { p.Capacity = 0 },
		func(p *Params) { p.Tau = 0.04 },
		func(p *Params) { p.Tau = 3.1 },
		func(p *Params) { p.Bits = 3 },
		func(p *Params) { p.ValueMin = -1 },
		func(p *Params) { p.ValueMax = -0.5 },
		func(p *Params) { p.Refine = true },
	}
	for i, f := range bad {
		p := DefaultParams(8, 0.5, 1)
		f(&p)
		if _, err := New(p); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestAddRejects(t *testing.T) {
	s := mustNew(t, DefaultParams(8, 0.5, 1))
	cases := []struct {
		v    []float32
		val  float64
		want error
	}{
		{basis(7, 0), 0.5, quant.ErrDim},
		{[]float32{float32(math.NaN()), 0, 0, 0, 0, 0, 0, 0}, 0.5, quant.ErrNonFinite},
		{make([]float32, 8), 0.5, quant.ErrZeroNorm},
		{basis(8, 0), 2, ErrValue},
		{basis(8, 0), -1, ErrValue},
		{basis(8, 0), math.NaN(), ErrValue},
	}
	for i, c := range cases {
		if err := s.Add(c.v, c.val, 1); !errors.Is(err, c.want) {
			t.Errorf("case %d: got %v, want %v", i, err, c.want)
		}
	}
	st := s.Stats()
	if st.W != 0 || st.Live != 0 {
		t.Errorf("rejected input changed state: %+v", st)
	}
	if r := st.Rejected; r.Dim != 1 || r.NonFinite != 1 || r.ZeroNorm != 1 || r.Value != 3 {
		t.Errorf("rejections %+v", r)
	}
}

func TestAddBatchAllOrNothing(t *testing.T) {
	s := mustNew(t, DefaultParams(8, 0.5, 1))
	vecs := [][]float32{basis(8, 0), basis(8, 1), basis(8, 2)}
	err := s.AddBatch(vecs, []float64{0.1, 0.2, 5}, []uint64{1, 2, 3})
	var be *BatchError
	if !errors.As(err, &be) || be.Index != 2 || !errors.Is(err, ErrValue) {
		t.Fatalf("got %v", err)
	}
	if st := s.Stats(); st.W != 0 || st.Live != 0 || st.Rejected.Value != 1 {
		t.Fatalf("partial apply: %+v", st)
	}
	if err := s.AddBatch(vecs, []float64{0.1, 0.2, 0.3}, []uint64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.W != 3 || st.Live != 3 {
		t.Fatalf("after good batch: %+v", st)
	}
	if err := s.AddBatch(vecs, []float64{1}, []uint64{1}); err != ErrBatchLength {
		t.Fatalf("length mismatch: %v", err)
	}
}

func TestAmbiguousGoesToQuarantine(t *testing.T) {
	p := DefaultParams(1024, 0.8, 3)
	p.Band = 0.2 // confident <= 0.6, ambiguous (0.6, 1.0]
	s := mustNew(t, p)
	mustAdd(t, s, plane(1024, 0), 1, 1)
	mustAdd(t, s, plane(1024, 1.6), 1, 2)
	mustAdd(t, s, plane(1024, 0.8), 0.5, 3) // 0.8 from both
	st := s.Stats()
	if st.Live != 2 || st.W != 2 || st.Wq != 1 || st.WqSum != 0.5 {
		t.Fatalf("stats %+v", st)
	}
	for _, e := range s.TopK(0, ByCount) {
		if e.Ambiguous != 1 || e.AmbiguousSum != 0.5 {
			t.Errorf("entity %d: ambiguous %v/%v, want 1/0.5", e.ID, e.Ambiguous, e.AmbiguousSum)
		}
		if e.Count.Lower != 1 || e.Count.Upper != 1 {
			t.Errorf("quarantine leaked into count: %+v", e.Count)
		}
	}
}

func TestProbationPromote(t *testing.T) {
	p := DefaultParams(1024, 0.8, 5)
	p.Probation = 2
	s := mustNew(t, p)
	mustAdd(t, s, plane(1024, 0), 1, 1)
	mustAdd(t, s, plane(1024, 0.1), 1, 2)
	if e := s.TopK(1, ByCount)[0]; !e.Provisional {
		t.Fatal("should be provisional with 1 member")
	}
	mustAdd(t, s, plane(1024, 0.1), 1, 2) // same item ID: not a new member
	mustAdd(t, s, plane(1024, -0.1), 1, 3)
	if e := s.TopK(1, ByCount)[0]; e.Provisional || e.Count.Lower != 4 {
		t.Fatalf("after 2 members: %+v", e)
	}
	if st := s.Stats(); st.Promoted != 1 || st.Dissolved != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestProbationDissolve(t *testing.T) {
	p := DefaultParams(1024, 0.8, 5)
	p.Probation = 2
	s := mustNew(t, p)
	mustAdd(t, s, plane(1024, 0), 1, 1)
	mustAdd(t, s, plane(1024, 0.5), 1, 2)
	mustAdd(t, s, plane(1024, -0.5), 1, 3) // members are 1.0 apart > tau
	st := s.Stats()
	if st.Live != 0 || st.Dissolved != 1 || st.W != 0 || st.Wq != 3 || st.Floor < 3 {
		t.Fatalf("stats %+v", st)
	}
	if c, _ := s.Unmonitored(); c.Upper < 3 {
		t.Errorf("unmonitored bound %v < 3", c.Upper)
	}
}

// Re-anchor is unreachable during ingest (SPEC §5.1), so it is tested
// through a merge that fuses two provisional counters.
func TestProbationReAnchorViaMerge(t *testing.T) {
	p := DefaultParams(1024, 0.8, 5)
	p.Probation = 3
	a, b := mustNew(t, p), mustNew(t, p)
	mustAdd(t, a, plane(1024, 0), 1, 1)
	mustAdd(t, a, plane(1024, 0.5), 1, 2)
	mustAdd(t, b, plane(1024, 0.5), 1, 3)
	mustAdd(t, b, plane(1024, 1.0), 1, 4)
	mustAdd(t, b, plane(1024, 1.0), 1, 5)
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	st := a.Stats()
	if st.Live != 1 || st.Fused != 1 || st.ReAnchored != 1 || st.Promoted != 1 || st.W != 4 || st.Wq != 1 {
		t.Fatalf("stats %+v", st)
	}
	e, ok := a.Lookup(plane(1024, 0.5))
	if !ok || e.Provisional || e.Count.Lower != 4 || e.Ambiguous != 1 {
		t.Fatalf("entity %+v ok=%v", e, ok)
	}
	for _, x := range e.Exemplars {
		if x == 1 {
			t.Error("re-anchored seed item still an exemplar")
		}
	}
}

func TestMergeRejects(t *testing.T) {
	a := mustNew(t, DefaultParams(8, 0.5, 1))
	if err := a.Merge(a); err != ErrSelfMerge {
		t.Errorf("self: %v", err)
	}
	p := DefaultParams(8, 0.5, 2)
	if err := a.Merge(mustNew(t, p)); err != ErrIncompatible {
		t.Errorf("seed: %v", err)
	}
	p = DefaultParams(8, 0.5, 1)
	p.ValueMax = 2
	if err := a.Merge(mustNew(t, p)); err != ErrIncompatible {
		t.Errorf("value range: %v", err)
	}
}

func TestExemplarsDistinctAndBounded(t *testing.T) {
	p := DefaultParams(64, 0.5, 9)
	p.Exemplars = 4
	s := mustNew(t, p)
	for i := 0; i < 50; i++ {
		mustAdd(t, s, basis(64, 0), 1, uint64(i%10))
	}
	e := s.TopK(1, ByCount)[0]
	if len(e.Exemplars) != 4 {
		t.Fatalf("exemplars %v", e.Exemplars)
	}
	seen := map[uint64]bool{}
	for _, x := range e.Exemplars {
		if seen[x] {
			t.Fatalf("duplicate exemplar %d", x)
		}
		seen[x] = true
	}
}

func TestDeterministic(t *testing.T) {
	w := makeWorld(1, 256, 30, 2000, 0.1)
	build := func() []Entity {
		p := DefaultParams(256, 0.6, 77)
		p.Capacity = 16
		s := mustNew(t, p)
		for _, it := range w.items {
			mustAdd(t, s, it.vec, it.val, it.id)
		}
		return s.TopK(0, ByCount)
	}
	if a, b := build(), build(); !reflect.DeepEqual(a, b) {
		t.Fatal("same input and seed gave different results")
	}
}

func TestConcurrentUse(t *testing.T) {
	w := makeWorld(2, 128, 10, 2000, 0.1)
	p := DefaultParams(128, 0.6, 3)
	p.Capacity = 8
	s, other := mustNew(t, p), mustNew(t, p)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := g; i < len(w.items); i += 4 {
				it := w.items[i]
				_ = s.Add(it.vec, it.val, it.id)
				_ = other.Add(it.vec, it.val, it.id)
				if i%100 == 0 {
					s.TopK(3, ByMean)
					s.Lookup(it.vec)
					_, _ = s.Diff(other)
				}
			}
		}(g)
	}
	wg.Wait()
	if st := s.Stats(); st.W+st.Wq != uint64(len(w.items)) {
		t.Fatalf("lost items: %+v", st)
	}
}
