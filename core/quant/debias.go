package quant

//go:generate go run gen_debias.go

import "math"

// Multi-bit codes estimate the angle from the cosine between quantised
// vectors, which is biased towards 0. For jointly Gaussian coordinates with
// correlation r the quantised cosine converges to a fixed increasing function
// g(r) (Bussgang). gen_debias.go tabulates g per bit width into
// debias_table.go, and debias inverts it by interpolation.

func stepSigma(b uint8) float64 {
	switch b {
	case 2:
		return step2
	case 4:
		return step4
	}
	return 0
}

func debias(b uint8, c float64) float64 {
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
