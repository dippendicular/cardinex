package quant

import (
	"math"
	"math/rand/v2"
	"testing"
)

func randVec(r *rand.Rand, d int) []float32 {
	v := make([]float32, d)
	for i := range v {
		v[i] = float32(r.NormFloat64())
	}
	return v
}

func angle(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return math.Acos(math.Max(-1, math.Min(1, dot/math.Sqrt(na*nb))))
}

func TestPaddedDim(t *testing.T) {
	for _, c := range []struct{ d, want int }{{1, 1}, {384, 512}, {768, 1024}, {1024, 1024}} {
		e, err := NewEncoder(c.d, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if e.PaddedDim() != c.want {
			t.Errorf("dim %d: padded %d, want %d", c.d, e.PaddedDim(), c.want)
		}
	}
}

func TestRotationPreservesAngles(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	e, _ := NewEncoder(384, 1, 42)
	for i := 0; i < 50; i++ {
		a, b := randVec(r, 384), randVec(r, 384)
		ya, _ := e.Rotate(a, nil)
		yb, _ := e.Rotate(b, nil)
		var dot, n float64
		for j := range ya {
			dot += ya[j] * yb[j]
			n += ya[j] * ya[j]
		}
		if math.Abs(n-1) > 1e-9 {
			t.Fatalf("rotated norm %v", n)
		}
		if got, want := math.Acos(math.Max(-1, math.Min(1, dot))), angle(a, b); math.Abs(got-want) > 1e-9 {
			t.Fatalf("angle %v, want %v", got, want)
		}
	}
}

func TestEncodeDeterministic(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	v := randVec(r, 300)
	for _, b := range []uint8{1, 2, 4} {
		e1, _ := NewEncoder(300, b, 7)
		e2, _ := NewEncoder(300, b, 7)
		c1, _ := e1.Encode(v)
		c2, _ := e2.Encode(v)
		if !c1.Equal(c2) {
			t.Fatalf("bits %d: codes differ", b)
		}
		if d := e1.Distance(c1, c1); d > 1e-12 {
			t.Fatalf("bits %d: self distance %v", b, d)
		}
	}
}

func TestDistanceApproximatesAngle(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for _, b := range []uint8{1, 2, 4} {
		e, _ := NewEncoder(1024, b, 9)
		var worst float64
		for i := 0; i < 200; i++ {
			a := randVec(r, 1024)
			// b2 at a controlled angle from a.
			n := randVec(r, 1024)
			bv := make([]float32, 1024)
			mix := r.Float64()
			for j := range bv {
				bv[j] = float32((1-mix)*float64(a[j]) + mix*float64(n[j]))
			}
			ca, _ := e.Encode(a)
			cb, _ := e.Encode(bv)
			worst = math.Max(worst, math.Abs(e.Distance(ca, cb)-angle(a, bv)))
		}
		limit := map[uint8]float64{1: 0.2, 2: 0.1, 4: 0.05}[b]
		if worst > limit {
			t.Errorf("bits %d: worst error %.3f > %.3f", b, worst, limit)
		}
		t.Logf("bits %d: worst |est-true| = %.4f rad over 200 pairs", b, worst)
	}
}

func TestEncodeRejects(t *testing.T) {
	e, _ := NewEncoder(4, 1, 1)
	if _, err := e.Encode([]float32{1, 2, 3}); err != ErrDim {
		t.Errorf("dim: %v", err)
	}
	if _, err := e.Encode([]float32{1, float32(math.NaN()), 0, 0}); err != ErrNonFinite {
		t.Errorf("nan: %v", err)
	}
	if _, err := e.Encode([]float32{1, float32(math.Inf(1)), 0, 0}); err != ErrNonFinite {
		t.Errorf("inf: %v", err)
	}
	if _, err := e.Encode([]float32{0, 0, 0, 0}); err != ErrZeroNorm {
		t.Errorf("zero: %v", err)
	}
	if _, err := NewEncoder(4, 3, 1); err != ErrBadBits {
		t.Errorf("bits: %v", err)
	}
}
