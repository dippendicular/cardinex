package bench

import (
	"fmt"
	"math"
	"testing"

	"github.com/dippendicular/cardinex/pkg/bloom"
	"github.com/dippendicular/cardinex/pkg/cms"
	"github.com/dippendicular/cardinex/pkg/hll"
)

var sizes = []int{10000, 100000, 1000000}

func BenchmarkSketches(b *testing.B) {
	for _, n := range sizes {
		b.Run(fmt.Sprintf("hll_%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s, _ := hll.New(14)
				for j := 0; j < n; j++ {
					s.Add([]byte(fmt.Sprintf("u:%d", j)))
				}
				_ = s.Count()
			}
		})
		b.Run(fmt.Sprintf("cms_%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s, _ := cms.New(2720, 5)
				for j := 0; j < n; j++ {
					s.Add([]byte(fmt.Sprintf("u:%d", j%1000)))
				}
				_ = s.Estimate([]byte("u:42"))
			}
		})
		b.Run(fmt.Sprintf("bloom_%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s, _ := bloom.New(n, 0.01)
				for j := 0; j < n; j++ {
					s.Add([]byte(fmt.Sprintf("u:%d", j)))
				}
				_ = s.Contains([]byte("u:42"))
			}
		})
	}
}

func TestMarkdownBenchmarkTable(t *testing.T) {
	t.Log("| Structure | Elements | Memory/Element | Theoretical Error | Actual Error |")
	t.Log("|---|---:|---:|---:|---:|")
	for _, n := range []int{10000, 100000} {
		h, _ := hll.New(14)
		for i := 0; i < n; i++ {
			h.Add([]byte(fmt.Sprintf("u:%d", i)))
		}
		actual := math.Abs(float64(h.Count())-float64(n)) / float64(n)
		t.Logf("| HyperLogLog | %d | %.3f B | %.4f | %.4f |", n, float64(h.MemoryBytes())/float64(n), h.ErrorRate(), actual)

		c, _ := cms.New(2720, 5)
		for i := 0; i < n; i++ {
			c.Add([]byte(fmt.Sprintf("u:%d", i%1000)))
		}
		actualCMS := float64(c.Estimate([]byte("u:42"))-uint64(n/1000)) / float64(n)
		t.Logf("| Count-Min Sketch | %d | %.3f B | %.4f | %.4f |", n, float64(c.MemoryBytes())/float64(n), c.Epsilon(), actualCMS)

		bf, _ := bloom.New(n, 0.01)
		for i := 0; i < n; i++ {
			bf.Add([]byte(fmt.Sprintf("u:%d", i)))
		}
		falseHits := 0
		trials := 10000
		for i := 0; i < trials; i++ {
			if bf.Contains([]byte(fmt.Sprintf("z:%d", i))) {
				falseHits++
			}
		}
		t.Logf("| Bloom Filter | %d | %.3f B | %.4f | %.4f |", n, float64(bf.MemoryBytes())/float64(n), bf.FalsePositiveRate(), float64(falseHits)/float64(trials))
	}
}
