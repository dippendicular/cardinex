package semantic

import (
	"math"
	"testing"
)

// bruteMean is the min/max of (G+x)/(g+y) over integer y in [0, I] and
// x in {a·y, b·y}, which is where the extremes lie for fixed y.
func bruteMean(g uint64, G float64, I uint64, a, b float64) (lo, hi float64, ok bool) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for y := uint64(0); y <= I; y++ {
		if g+y == 0 {
			continue
		}
		for _, x := range []float64{a * float64(y), b * float64(y)} {
			m := (G + x) / float64(g+y)
			lo, hi = math.Min(lo, m), math.Max(hi, m)
			ok = true
		}
	}
	return
}

func TestMeanIntervalMatchesBruteForce(t *testing.T) {
	for _, r := range [][2]float64{{0, 1}, {0, 0}, {2, 5}} {
		a, b := r[0], r[1]
		for _, g := range []uint64{0, 1, 2, 10} {
			for _, I := range []uint64{0, 1, 5, 100} {
				gs := []float64{a * float64(g), b * float64(g), (a + b) / 2 * float64(g)}
				for _, G := range gs {
					got := MeanInterval(g, G, I, a, b)
					if g == 0 {
						if !got.Empty || got.Lower != a || got.Upper != b {
							t.Errorf("g=0 I=%d [%v,%v]: got %+v, want [a,b] empty", I, a, b, got)
						}
						continue
					}
					lo, hi, _ := bruteMean(g, G, I, a, b)
					if math.Abs(got.Lower-lo) > 1e-12 || math.Abs(got.Upper-hi) > 1e-12 {
						t.Errorf("g=%d G=%v I=%d [%v,%v]: got [%v,%v], brute [%v,%v]", g, G, I, a, b, got.Lower, got.Upper, lo, hi)
					}
					if got.Value < got.Lower || got.Value > got.Upper {
						t.Errorf("value %v outside [%v,%v]", got.Value, got.Lower, got.Upper)
					}
				}
			}
		}
	}
}

func TestMeanIntervalEdgeCases(t *testing.T) {
	if e := MeanInterval(4, 2, 0, 0, 1); e.Lower != 0.5 || e.Upper != 0.5 || e.Value != 0.5 {
		t.Errorf("I=0: %+v", e)
	}
	if e := MeanInterval(0, 0, 7, 0, 1); !e.Empty || e.Lower != 0 || e.Upper != 1 || e.Value != 0.5 {
		t.Errorf("all inherited: %+v", e)
	}
	if e := MeanInterval(3, 6, 9, 2, 2); e.Lower != 2 || e.Upper != 2 || e.Value != 2 {
		t.Errorf("a=b: %+v", e)
	}
	// Rounding noise in G must not push the result outside [a, b].
	if e := MeanInterval(3, 3.0000000001, 0, 0, 1); e.Upper > 1 || e.Value > 1 {
		t.Errorf("clamp: %+v", e)
	}
}

func TestCountAndSumIntervals(t *testing.T) {
	c := CountInterval(3, 5)
	if c.Lower != 3 || c.Upper != 8 || c.Value != 8 {
		t.Errorf("count: %+v", c)
	}
	s := SumInterval(1.5, 0.5, 4, 2)
	if s.Lower != 1.5 || s.Upper != 9.5 || s.Value != 2 {
		t.Errorf("sum: %+v", s)
	}
	if s := SumInterval(1, 50, 2, 1); s.Value != 3 {
		t.Errorf("sum value not clamped: %+v", s)
	}
}

// TestSumUpperBoundCounterexample replays docs/ESTIMATORS.md §3. The brief's
// bound G + inherited_sum gives 1 for X; the true sum is 2.
func TestSumUpperBoundCounterexample(t *testing.T) {
	p := DefaultParams(64, 0.5, 11)
	p.Capacity, p.Probation = 2, 0
	sk, _ := New(p)
	X, Z, Y, W := basis(64, 0), basis(64, 1), basis(64, 2), basis(64, 3)
	steps := []struct {
		v   []float32
		val float64
	}{{X, 1}, {Z, 0}, {Z, 0}, {Y, 0}, {Y, 0}, {Y, 0}, {W, 0}, {X, 1}}
	for i, s := range steps {
		if err := sk.Add(s.v, s.val, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if st := sk.Stats(); st.Wq != 0 || st.Live != 2 {
		t.Fatalf("setup misclassified: %+v", st)
	}
	e, ok := sk.Lookup(X)
	if !ok {
		t.Fatal("X has no counter")
	}
	if e.Sum.Value != 1 {
		t.Errorf("G + inhSum = %v, want 1 (the brief's bound)", e.Sum.Value)
	}
	if e.Sum.Lower > 2 || e.Sum.Upper < 2 {
		t.Errorf("sum interval [%v,%v] misses true sum 2", e.Sum.Lower, e.Sum.Upper)
	}
	if e.Count.Lower != 1 || e.Count.Upper != 4 {
		t.Errorf("count interval %+v, want [1,4]", e.Count)
	}
}
