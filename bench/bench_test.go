// Package bench holds the microbenchmarks (IMPLEMENTATION.md §10, Phase 1):
// semantic add, add-batch, merge, top-k and serialize at d = 384, 768, 1024
// and capacity 1k–16k, plus the classic sketches.
//
//	go test -bench . -benchmem ./bench/
package bench

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
)

var (
	dims       = []int{384, 768, 1024}
	capacities = []int{1024, 4096, 16384}
)

func vectors(n, d int, seed uint64) [][]float32 {
	r := rand.New(rand.NewPCG(seed, 1))
	out := make([][]float32, n)
	for i := range out {
		v := make([]float32, d)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		out[i] = v
	}
	return out
}

func newSemantic(b *testing.B, d, capacity int) *semantic.Sketch {
	p := semantic.DefaultParams(d, 0.6, 1)
	p.Capacity = capacity
	s, err := semantic.New(p)
	if err != nil {
		b.Fatal(err)
	}
	return s
}

// filled returns a sketch with every slot in use, so Add pays a full scan.
func filled(b *testing.B, d, capacity int) *semantic.Sketch {
	s := newSemantic(b, d, capacity)
	vs := vectors(capacity, d, 2)
	for i, v := range vs {
		_ = s.Add(v, 0.5, uint64(i))
	}
	return s
}

func BenchmarkSemanticAdd(b *testing.B) {
	for _, d := range dims {
		for _, c := range capacities {
			b.Run(fmt.Sprintf("d=%d/cap=%d", d, c), func(b *testing.B) {
				s := filled(b, d, c)
				vs := vectors(256, d, 3)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = s.Add(vs[i%len(vs)], 0.5, uint64(i))
				}
			})
		}
	}
}

func BenchmarkSemanticAddBatch(b *testing.B) {
	const batch = 256
	for _, d := range dims {
		b.Run(fmt.Sprintf("d=%d/cap=4096", d), func(b *testing.B) {
			s := filled(b, d, 4096)
			vs := vectors(batch, d, 4)
			vals := make([]float64, batch)
			ids := make([]uint64, batch)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := range ids {
					ids[j] = uint64(i*batch + j)
				}
				_ = s.AddBatch(vs, vals, ids)
			}
			b.ReportMetric(float64(b.N*batch)/b.Elapsed().Seconds(), "items/s")
		})
	}
}

func BenchmarkSemanticMerge(b *testing.B) {
	for _, c := range capacities {
		b.Run(fmt.Sprintf("d=768/cap=%d", c), func(b *testing.B) {
			x, y := filled(b, 768, c), filled(b, 768, c)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = x.Clone().Merge(y)
			}
		})
	}
}

func BenchmarkSemanticTopK(b *testing.B) {
	for _, c := range capacities {
		b.Run(fmt.Sprintf("cap=%d", c), func(b *testing.B) {
			s := filled(b, 384, c)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.TopK(100, semantic.ByCount)
			}
		})
	}
}

func BenchmarkSemanticSerialize(b *testing.B) {
	for _, c := range capacities {
		b.Run(fmt.Sprintf("d=768/cap=%d", c), func(b *testing.B) {
			s := filled(b, 768, c)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf, _ := s.MarshalBinary()
				b.SetBytes(int64(len(buf)))
			}
		})
	}
}

func keys(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte("key:" + strconv.Itoa(i))
	}
	return out
}

func BenchmarkClassic(b *testing.B) {
	ks := keys(1 << 16)
	b.Run("hll/add", func(b *testing.B) {
		h, _ := classic.NewHLL(14)
		for i := 0; i < b.N; i++ {
			h.AddKey(ks[i&(len(ks)-1)])
		}
	})
	b.Run("cms/add", func(b *testing.B) {
		c, _ := classic.NewCMS(2720, 5, false)
		for i := 0; i < b.N; i++ {
			c.AddKey(ks[i&(len(ks)-1)], 1)
		}
	})
	b.Run("cms-cu/add", func(b *testing.B) {
		c, _ := classic.NewCMS(2720, 5, true)
		for i := 0; i < b.N; i++ {
			c.AddKey(ks[i&(len(ks)-1)], 1)
		}
	})
	b.Run("bloom/add", func(b *testing.B) {
		f, _ := classic.NewBloom(1_000_000, 0.01)
		for i := 0; i < b.N; i++ {
			f.AddKey(ks[i&(len(ks)-1)])
		}
	})
}
