// Package quant turns embeddings into compact codes whose pairwise angular
// distance can be estimated cheaply.
//
// The pipeline is normalise -> zero-pad to a power of two -> seeded randomized
// Hadamard rotation (three rounds of a random-sign diagonal followed by a fast
// Walsh-Hadamard transform) -> scalar quantisation at 1, 2 or 4 bits per
// padded dimension. See docs/SPEC.md §3.
package quant

import (
	"errors"
	"math"
	"math/bits"
)

var (
	ErrDim        = errors.New("quant: vector length does not match dim")
	ErrNonFinite  = errors.New("quant: vector has NaN or Inf component")
	ErrZeroNorm   = errors.New("quant: vector has zero norm")
	ErrBadBits    = errors.New("quant: bits must be 1, 2 or 4")
	ErrBadDim     = errors.New("quant: dim must be positive")
	ErrCodeLength = errors.New("quant: code length mismatch")
)

const rounds = 3

// Code is a quantised, rotated unit vector. Norm2 is the squared norm of the
// odd-integer levels and is zero for 1-bit codes.
type Code struct {
	Words []uint64
	Norm2 int64
}

// Clone returns a deep copy.
func (c Code) Clone() Code {
	return Code{Words: append([]uint64(nil), c.Words...), Norm2: c.Norm2}
}

// Equal reports whether two codes are identical.
func (c Code) Equal(o Code) bool {
	if c.Norm2 != o.Norm2 || len(c.Words) != len(o.Words) {
		return false
	}
	for i := range c.Words {
		if c.Words[i] != o.Words[i] {
			return false
		}
	}
	return true
}

// Encoder holds the seeded rotation. It is immutable after construction and
// safe for concurrent use.
type Encoder struct {
	dim    int
	padded int
	bits   uint8
	signs  [rounds][]uint64 // bit set = negate that coordinate
	scale  float64          // 1/sqrt(D), applied after each FWHT
	thresh float64          // quantisation range t = half the level count times the step
}

// NewEncoder builds the rotation for vectors of length dim.
func NewEncoder(dim int, b uint8, seed uint64) (*Encoder, error) {
	if dim <= 0 {
		return nil, ErrBadDim
	}
	if b != 1 && b != 2 && b != 4 {
		return nil, ErrBadBits
	}
	d := 1
	for d < dim {
		d <<= 1
	}
	// Rotated coordinates are close to N(0, 1/D). The step is the MSE-optimal
	// uniform step for a Gaussian: 0.996 sigma at 2 bits, 0.335 sigma at 4 bits.
	sigma := 1 / math.Sqrt(float64(d))
	e := &Encoder{dim: dim, padded: d, bits: b, scale: sigma, thresh: stepSigma(b) * sigma * float64(int(1)<<b) / 2}
	state := seed ^ 0x6a09e667f3bcc909
	for r := 0; r < rounds; r++ {
		w := make([]uint64, (d+63)/64)
		for i := range w {
			w[i] = SplitMix64(&state)
		}
		e.signs[r] = w
	}
	return e, nil
}

func (e *Encoder) Dim() int       { return e.dim }
func (e *Encoder) PaddedDim() int { return e.padded }
func (e *Encoder) Bits() uint8    { return e.bits }

// Rotate normalises vec and applies the seeded rotation, writing PaddedDim
// values into out (allocated if nil or too short).
func (e *Encoder) Rotate(vec []float32, out []float64) ([]float64, error) {
	if len(vec) != e.dim {
		return nil, ErrDim
	}
	var ss float64
	for _, v := range vec {
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, ErrNonFinite
		}
		ss += float64(f * f)
	}
	if ss == 0 {
		return nil, ErrZeroNorm
	}
	norm := math.Sqrt(ss)
	if len(out) < e.padded {
		out = make([]float64, e.padded)
	}
	out = out[:e.padded]
	for i, v := range vec {
		out[i] = float64(v) / norm
	}
	for i := e.dim; i < e.padded; i++ {
		out[i] = 0
	}
	for r := 0; r < rounds; r++ {
		s := e.signs[r]
		for i := range out {
			if s[i>>6]>>(uint(i)&63)&1 == 1 {
				out[i] = -out[i]
			}
		}
		fwht(out)
		for i := range out {
			out[i] *= e.scale
		}
	}
	return out, nil
}

// Encode validates, rotates and quantises vec.
func (e *Encoder) Encode(vec []float32) (Code, error) {
	y, err := e.Rotate(vec, nil)
	if err != nil {
		return Code{}, err
	}
	return e.quantize(y), nil
}

func (e *Encoder) quantize(y []float64) Code {
	if e.bits == 1 {
		w := make([]uint64, (e.padded+63)/64)
		for i, v := range y {
			if v >= 0 {
				w[i>>6] |= 1 << (uint(i) & 63)
			}
		}
		return Code{Words: w}
	}
	b := int(e.bits)
	levels := 1 << b
	per := 64 / b
	w := make([]uint64, (e.padded+per-1)/per)
	step := 2 * e.thresh / float64(levels)
	var n2 int64
	for i, v := range y {
		q := int(math.Floor((v + e.thresh) / step))
		if q < 0 {
			q = 0
		} else if q >= levels {
			q = levels - 1
		}
		s := int64(2*q + 1 - levels)
		n2 += s * s
		w[i/per] |= uint64(q) << (uint(i%per) * uint(b))
	}
	return Code{Words: w, Norm2: n2}
}

// Distance estimates the angular distance in radians between two codes made
// by this encoder.
func (e *Encoder) Distance(p, q Code) float64 {
	if e.bits == 1 {
		h := 0
		for i := range p.Words {
			h += bits.OnesCount64(p.Words[i] ^ q.Words[i])
		}
		return math.Pi * float64(h) / float64(e.padded)
	}
	b := uint(e.bits)
	levels := int64(1) << b
	per := 64 / int(b)
	mask := uint64(levels - 1)
	var dot int64
	for i := 0; i < e.padded; i++ {
		sh := uint(i%per) * b
		sp := 2*int64(p.Words[i/per]>>sh&mask) + 1 - levels
		sq := 2*int64(q.Words[i/per]>>sh&mask) + 1 - levels
		dot += sp * sq
	}
	c := float64(dot) / math.Sqrt(float64(p.Norm2)*float64(q.Norm2))
	return math.Acos(debias(e.bits, c))
}

func fwht(x []float64) {
	n := len(x)
	for h := 1; h < n; h <<= 1 {
		for i := 0; i < n; i += h << 1 {
			for j := i; j < i+h; j++ {
				a, b := x[j], x[j+h]
				x[j], x[j+h] = a+b, a-b
			}
		}
	}
}

// SplitMix64 advances state and returns the next value.
func SplitMix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	return Mix64(*state)
}

// Mix64 is the SplitMix64 finaliser, used as a seeded 64-bit hash.
func Mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Metric estimates the angular distance in radians between two codes.
// Encoder implements it with the symmetric estimate of docs/SPEC.md §3.3; an
// asymmetric (RaBitQ-style) estimator can be added behind the same interface.
type Metric interface {
	Distance(p, q Code) float64
}

var _ Metric = (*Encoder)(nil)
