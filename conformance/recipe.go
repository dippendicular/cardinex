// Package conformance defines the cross-language conformance suite. Every SDK
// rebuilds each case in spec.json from the same input recipe, checks that its
// serialized bytes equal conformance/golden/<case>.cdnx exactly, then decodes
// the golden file and checks the expected answers.
//
// Input recipe (must be reproduced exactly in every language):
//
//	keys:    key(i) = prefix + decimal(i % modulo)   (modulo 0 means no modulo), UTF-8
//	vectors: for item i in [0, items), entity j = i % entities, dimension t in [0, dim):
//	           noise = 0.01 * float64((i*7 + t*13) % 11 - 5) / 5      (float64, left to right)
//	           v[t]  = float32((t == j % dim ? 1.0 : 0.0) + noise)
//	         value(i) = float64((i*37) % 101) / 100
//	         id(i)    = i + 1
//
// Items are added one at a time, in order.
package conformance

import "strconv"

// KeyOps describes a key stream.
type KeyOps struct {
	Prefix string `json:"prefix"`
	Count  int    `json:"count"`
	Modulo int    `json:"modulo,omitempty"`
}

// Keys expands the recipe.
func (k KeyOps) Keys() [][]byte {
	out := make([][]byte, k.Count)
	for i := range out {
		n := i
		if k.Modulo > 0 {
			n = i % k.Modulo
		}
		out[i] = []byte(k.Prefix + strconv.Itoa(n))
	}
	return out
}

// VectorOps describes a vector stream.
type VectorOps struct {
	Dim      int `json:"dim"`
	Entities int `json:"entities"`
	Items    int `json:"items"`
}

// Vector returns item i's vector.
func (v VectorOps) Vector(i int) []float32 {
	j := i % v.Entities
	out := make([]float32, v.Dim)
	for t := range out {
		noise := 0.01 * float64((i*7+t*13)%11-5) / 5
		base := 0.0
		if t == j%v.Dim {
			base = 1
		}
		out[t] = float32(base + noise)
	}
	return out
}

// Value returns item i's value.
func Value(i int) float64 { return float64((i*37)%101) / 100 }

// ID returns item i's ID.
func ID(i int) uint64 { return uint64(i + 1) }
