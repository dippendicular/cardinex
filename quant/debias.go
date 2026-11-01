package quant

import (
	"math"
	"sync"
)

// Multi-bit codes estimate the angle from the cosine between quantised
// vectors, which is biased towards 0. For jointly Gaussian coordinates with
// correlation r the quantised cosine converges to a fixed increasing function
// g(r) (Bussgang). We tabulate g once per bit width and invert it.
//
// The table is computed with math.Erfc at first use. Phase 1 replaces this
// with generated constants so the table is bit-identical on every platform.

const tableSize = 400

var (
	tableOnce [5]sync.Once
	tables    [5][]float64 // tables[b][k] = g(-1 + 2k/tableSize)
)

func stepSigma(b uint8) float64 {
	switch b {
	case 2:
		return 0.996
	case 4:
		return 0.335
	}
	return 0
}

func debias(b uint8, c float64) float64 {
	tableOnce[b].Do(func() { tables[b] = buildTable(b) })
	g := tables[b]
	if c <= g[0] {
		return -1
	}
	if c >= g[tableSize] {
		return 1
	}
	lo, hi := 0, tableSize
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if g[mid] <= c {
			lo = mid
		} else {
			hi = mid
		}
	}
	frac := (c - g[lo]) / (g[hi] - g[lo])
	r := -1 + (float64(lo)+frac)*2/tableSize
	return math.Max(-1, math.Min(1, r))
}

// buildTable computes g(r) = E[q(X)q(Y)] / E[q(X)^2] for standard normal X, Y
// with correlation r, where q maps to the odd-integer level of its bin.
func buildTable(b uint8) []float64 {
	levels := 1 << b
	step := stepSigma(b)
	edges := make([]float64, levels+1) // bin j is [edges[j], edges[j+1])
	edges[0], edges[levels] = math.Inf(-1), math.Inf(1)
	for j := 1; j < levels; j++ {
		edges[j] = (float64(j) - float64(levels)/2) * step
	}
	level := func(j int) float64 { return float64(2*j + 1 - levels) }
	phi := func(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }

	var q2 float64
	for j := 0; j < levels; j++ {
		q2 += level(j) * level(j) * (phi(edges[j+1]) - phi(edges[j]))
	}

	const n = 1600 // Simpson panels over [-8, 8]
	h := 16.0 / n
	g := make([]float64, tableSize+1)
	g[0], g[tableSize] = -1, 1
	for k := 1; k < tableSize; k++ {
		r := -1 + 2*float64(k)/tableSize
		sd := math.Sqrt(1 - r*r)
		var acc float64
		for i := 0; i <= n; i++ {
			x := -8 + float64(i)*h
			w := 2.0
			if i == 0 || i == n {
				w = 1
			} else if i%2 == 1 {
				w = 4
			}
			jx := levels - 1
			for j := 0; j < levels-1; j++ {
				if x < edges[j+1] {
					jx = j
					break
				}
			}
			var m float64 // E[q(Y) | X = x]
			for j := 0; j < levels; j++ {
				m += level(j) * (phi((edges[j+1]-r*x)/sd) - phi((edges[j]-r*x)/sd))
			}
			acc += w * math.Exp(-x*x/2) * level(jx) * m
		}
		g[k] = acc * h / 3 / math.Sqrt(2*math.Pi) / q2
	}
	return g
}
