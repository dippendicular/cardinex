package semantic

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

// Property tests on valid streams (docs/ESTIMATORS.md §7): entities on the
// sphere with radius rho, well separated, Zipf sizes. Every reported interval
// must contain the exact per-entity truth, on a single stream and after three
// merge-tree shapes.

type item struct {
	vec []float32
	val float64
	ent int
	id  uint64
}

type world struct {
	dim     int
	centers [][]float64
	items   []item
	minSep  float64
}

func unit(r *rand.Rand, d int) []float64 {
	v := make([]float64, d)
	var n float64
	for i := range v {
		v[i] = r.NormFloat64()
		n += v[i] * v[i]
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= n
	}
	return v
}

// perturb returns cos(phi)·c + sin(phi)·u for a random unit u orthogonal to c,
// so the angle to c is exactly phi.
func perturb(r *rand.Rand, c []float64, phi float64) []float32 {
	u := unit(r, len(c))
	var dot float64
	for i := range u {
		dot += u[i] * c[i]
	}
	var n float64
	for i := range u {
		u[i] -= dot * c[i]
		n += u[i] * u[i]
	}
	n = math.Sqrt(n)
	out := make([]float32, len(c))
	for i := range c {
		out[i] = float32(math.Cos(phi)*c[i] + math.Sin(phi)*u[i]/n)
	}
	return out
}

func makeWorld(seed uint64, dim, entities, n int, rho float64) world {
	r := rand.New(rand.NewPCG(seed, seed^0xabcdef))
	w := world{dim: dim, minSep: math.Pi}
	means := make([]float64, entities)
	cum := make([]float64, entities)
	var total float64
	for j := 0; j < entities; j++ {
		w.centers = append(w.centers, unit(r, dim))
		means[j] = r.Float64()
		total += 1 / math.Pow(float64(j+1), 1.1)
		cum[j] = total
	}
	for i := 0; i < entities; i++ {
		for j := i + 1; j < entities; j++ {
			var dot float64
			for k := 0; k < dim; k++ {
				dot += w.centers[i][k] * w.centers[j][k]
			}
			w.minSep = math.Min(w.minSep, math.Acos(dot))
		}
	}
	for i := 0; i < n; i++ {
		x := r.Float64() * total
		j := 0
		for cum[j] < x {
			j++
		}
		val := math.Max(0, math.Min(1, means[j]+(r.Float64()-0.5)*0.4))
		w.items = append(w.items, item{perturb(r, w.centers[j], r.Float64()*rho), val, j, uint64(i + 1)})
	}
	return w
}

func propParams(seed uint64) Params {
	p := DefaultParams(512, 0.6, seed)
	p.Capacity = 24
	return p
}

func checkBounds(t *testing.T, name string, s *Sketch, w world, items []item) {
	t.Helper()
	n := map[int]float64{}
	sum := map[int]float64{}
	owner := map[uint64]int{}
	for _, it := range items {
		n[it.ent]++
		sum[it.ent] += it.val
		owner[it.id] = it.ent
	}
	st := s.Stats()
	if st.Wq != 0 || st.Dissolved != 0 || st.ReAnchored != 0 || st.Aliased != 0 {
		t.Fatalf("%s: identity events on a valid stream: %+v", name, st)
	}
	if st.W != uint64(len(items)) {
		t.Fatalf("%s: W = %d, want %d", name, st.W, len(items))
	}
	const eps = 1e-9
	seen := map[int]bool{}
	var maxWidth float64
	for _, e := range s.TopK(0, ByCount) {
		if len(e.Exemplars) == 0 {
			t.Fatalf("%s: entity %d has no exemplars", name, e.ID)
		}
		ent := owner[e.Exemplars[0]]
		for _, x := range e.Exemplars {
			if owner[x] != ent {
				t.Fatalf("%s: counter %d mixes entities %d and %d", name, e.ID, ent, owner[x])
			}
		}
		if seen[ent] {
			t.Fatalf("%s: entity %d has two counters", name, ent)
		}
		seen[ent] = true
		tn, ts := n[ent], sum[ent]
		if tn < e.Count.Lower || tn > e.Count.Upper {
			t.Errorf("%s: entity %d count %v outside [%v,%v]", name, ent, tn, e.Count.Lower, e.Count.Upper)
		}
		if ts < e.Sum.Lower-eps || ts > e.Sum.Upper+eps {
			t.Errorf("%s: entity %d sum %v outside [%v,%v]", name, ent, ts, e.Sum.Lower, e.Sum.Upper)
		}
		if m := ts / tn; m < e.Mean.Lower-eps || m > e.Mean.Upper+eps {
			t.Errorf("%s: entity %d mean %v outside [%v,%v]", name, ent, m, e.Mean.Lower, e.Mean.Upper)
		}
		maxWidth = math.Max(maxWidth, e.Count.Upper-e.Count.Lower)
	}
	cu, _ := s.Unmonitored()
	for ent, c := range n {
		if !seen[ent] && c > cu.Upper {
			t.Errorf("%s: unmonitored entity %d count %v > Ū = %v", name, ent, c, cu.Upper)
		}
	}
	t.Logf("%s: live=%d entities=%d Ū=%v max count width=%v (W/m=%.0f)", name, st.Live, len(n), cu.Upper, maxWidth, float64(len(items))/float64(s.p.Capacity))
}

func TestValidStreamSingle(t *testing.T) {
	w := makeWorld(42, 512, 60, 6000, 0.1)
	p := propParams(7)
	// (V): tau - band >= 2rho + 2eta and tau + band < Delta - 2rho - 2eta.
	if w.minSep < 4*0.1+2*p.Band+0.3 {
		t.Fatalf("world not separated enough: min center angle %v", w.minSep)
	}
	s := mustNew(t, p)
	for _, it := range w.items {
		mustAdd(t, s, it.vec, it.val, it.id)
	}
	if st := s.Stats(); st.Live != p.Capacity {
		t.Fatalf("expected a full sketch with evictions, live=%d", st.Live)
	}
	checkBounds(t, "single", s, w, w.items)
}

func buildShards(t *testing.T, w world, k int, seed uint64) []*Sketch {
	r := rand.New(rand.NewPCG(seed, 99))
	shards := make([]*Sketch, k)
	for i := range shards {
		shards[i] = mustNew(t, propParams(7))
	}
	for _, it := range w.items {
		mustAdd(t, shards[r.IntN(k)], it.vec, it.val, it.id)
	}
	return shards
}

func TestValidStreamMergeTrees(t *testing.T) {
	w := makeWorld(43, 512, 60, 6000, 0.1)
	trees := map[string]func([]*Sketch, *rand.Rand) *Sketch{
		"sequential": func(sh []*Sketch, _ *rand.Rand) *Sketch {
			for _, x := range sh[1:] {
				if err := sh[0].Merge(x); err != nil {
					t.Fatal(err)
				}
			}
			return sh[0]
		},
		"balanced": func(sh []*Sketch, _ *rand.Rand) *Sketch {
			for len(sh) > 1 {
				var next []*Sketch
				for i := 0; i+1 < len(sh); i += 2 {
					if err := sh[i].Merge(sh[i+1]); err != nil {
						t.Fatal(err)
					}
					next = append(next, sh[i])
				}
				if len(sh)%2 == 1 {
					next = append(next, sh[len(sh)-1])
				}
				sh = next
			}
			return sh[0]
		},
		"random": func(sh []*Sketch, r *rand.Rand) *Sketch {
			for len(sh) > 1 {
				i := r.IntN(len(sh))
				j := r.IntN(len(sh) - 1)
				if j >= i {
					j++
				}
				if err := sh[i].Merge(sh[j]); err != nil {
					t.Fatal(err)
				}
				sh = append(sh[:j], sh[j+1:]...)
			}
			return sh[0]
		},
	}
	for _, name := range []string{"sequential", "balanced", "random"} {
		for _, k := range []int{2, 8, 16} {
			t.Run(fmt.Sprintf("%s/k=%d", name, k), func(t *testing.T) {
				shards := buildShards(t, w, k, uint64(k))
				merged := trees[name](shards, rand.New(rand.NewPCG(uint64(k), 5)))
				checkBounds(t, name, merged, w, w.items)
			})
		}
	}
}

func TestDiffContainsTruth(t *testing.T) {
	w := makeWorld(44, 512, 40, 4000, 0.1)
	half := w.items[:2000]
	a, b := mustNew(t, propParams(7)), mustNew(t, propParams(7))
	for _, it := range w.items {
		mustAdd(t, a, it.vec, it.val, it.id)
	}
	for _, it := range half {
		mustAdd(t, b, it.vec, it.val, it.id)
	}
	owner := map[uint64]int{}
	for _, it := range w.items {
		owner[it.id] = it.ent
	}
	entOf := func(s *Sketch) map[uint64]int {
		m := map[uint64]int{}
		for _, e := range s.TopK(0, ByCount) {
			m[e.ID] = owner[e.Exemplars[0]]
		}
		return m
	}
	ea, eb := entOf(a), entOf(b)
	count := func(items []item, ent int) float64 {
		c := 0.0
		for _, it := range items {
			if it.ent == ent {
				c++
			}
		}
		return c
	}
	ds, err := a.Diff(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		var ent int
		switch {
		case d.InA:
			ent = ea[d.IDA]
			if d.InB && eb[d.IDB] != ent {
				t.Fatalf("diff matched different entities %d and %d", ent, eb[d.IDB])
			}
		default:
			ent = eb[d.IDB]
		}
		truth := count(w.items, ent) - count(half, ent)
		if truth < d.Count.Lower || truth > d.Count.Upper {
			t.Errorf("entity %d: delta %v outside [%v,%v]", ent, truth, d.Count.Lower, d.Count.Upper)
		}
	}
}
