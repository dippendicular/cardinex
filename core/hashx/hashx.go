// Package hashx is the only hashing entry point for the classic sketches.
// It wraps XXH3-128 (github.com/zeebo/xxh3), whose output is fixed by the
// XXH3 specification and is identical on every platform.
package hashx

import "github.com/zeebo/xxh3"

// Hash128 returns the two 64-bit halves of XXH3-128 of b with the given seed.
func Hash128(b []byte, seed uint64) (hi, lo uint64) {
	h := xxh3.Hash128Seed(b, seed)
	return h.Hi, h.Lo
}

// Hash64 returns XXH3-64 of b with the given seed.
func Hash64(b []byte, seed uint64) uint64 {
	return xxh3.HashSeed(b, seed)
}

// DoubleHash returns the Kirsch–Mitzenmacher pair (h1, h2) from one XXH3-128
// pass. h2 is forced odd, so h1 + i·h2 visits distinct residues modulo any
// power of two. The family h1 + i·h2 is NOT pairwise independent; it only
// preserves the asymptotic false-positive rate of Bloom filters
// (Kirsch & Mitzenmacher 2006) and the Count-Min guarantee in practice.
func DoubleHash(b []byte, seed uint64) (h1, h2 uint64) {
	h1, h2 = Hash128(b, seed)
	return h1, h2 | 1
}
