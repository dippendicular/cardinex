package classic

import (
	"errors"
	"math"
	"math/bits"

	"github.com/dippendicular/cardinex/core/hashx"
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

// Bloom is a classic Bloom filter with Kirsch–Mitzenmacher double hashing.
// It tracks the number of set bits X, so the current false-positive rate
// (X/m)^k and a cardinality estimate −(m/k)·ln(1 − X/m) (Swamidass & Baldi
// 2007) are exact functions of the state rather than of an insert counter,
// and stay correct after duplicates and merges.
type Bloom struct {
	m    uint64 // bits
	k    uint32
	set  []uint64
	ones uint64
}

// MaxBloomBits bounds the filter at 1 GiB.
const MaxBloomBits = 1 << 33

var errBloomShape = errors.New("classic: bloom needs 0 < bits <= 2^33 and 0 < k <= 64")

// NewBloom sizes a filter for n keys at false-positive rate fpr.
func NewBloom(n int, fpr float64) (*Bloom, error) {
	if n <= 0 || !(fpr > 0 && fpr < 1) {
		return nil, errors.New("classic: bloom needs n > 0 and 0 < fpr < 1")
	}
	m := math.Ceil(-float64(n) * math.Log(fpr) / (math.Ln2 * math.Ln2))
	k := math.Max(1, math.Round(m/float64(n)*math.Ln2))
	if m > MaxBloomBits {
		return nil, errBloomShape
	}
	return NewBloomWithParams(uint64(m), uint32(math.Min(k, 64)))
}

// NewBloomWithParams builds a filter with m bits and k hash functions.
func NewBloomWithParams(m uint64, k uint32) (*Bloom, error) {
	if m == 0 || m > MaxBloomBits || k == 0 || k > 64 {
		return nil, errBloomShape
	}
	return &Bloom{m: m, k: k, set: make([]uint64, (m+63)/64)}, nil
}

func (b *Bloom) Bits() uint64      { return b.m }
func (b *Bloom) Hashes() uint32    { return b.k }
func (b *Bloom) SetBits() uint64   { return b.ones }
func (b *Bloom) MemoryBytes() int  { return len(b.set) * 8 }
func (b *Bloom) Kind() sketch.Kind { return sketch.KindBloom }

// AddKey inserts key.
func (b *Bloom) AddKey(key []byte) {
	h1, h2 := hashx.DoubleHash(key, 0)
	for i := uint64(0); i < uint64(b.k); i++ {
		pos := (h1 + i*h2) % b.m
		mask := uint64(1) << (pos & 63)
		if b.set[pos>>6]&mask == 0 {
			b.set[pos>>6] |= mask
			b.ones++
		}
	}
}

// Contains reports whether key may be in the set (no false negatives).
func (b *Bloom) Contains(key []byte) bool {
	h1, h2 := hashx.DoubleHash(key, 0)
	for i := uint64(0); i < uint64(b.k); i++ {
		pos := (h1 + i*h2) % b.m
		if b.set[pos>>6]&(1<<(pos&63)) == 0 {
			return false
		}
	}
	return true
}

// FalsePositiveRate is (X/m)^k for the current fill.
func (b *Bloom) FalsePositiveRate() float64 {
	return math.Pow(float64(b.ones)/float64(b.m), float64(b.k))
}

// EstimateCardinality returns −(m/k)·ln(1 − X/m). The interval is a ±2σ
// approximation from the fill-ratio variance; it is probabilistic. A
// saturated filter returns Upper = +Inf.
func (b *Bloom) EstimateCardinality() sketch.Estimate {
	m, k, x := float64(b.m), float64(b.k), float64(b.ones)
	if b.ones == b.m {
		lo := -(m / k) * math.Log(1/m)
		return sketch.Estimate{Value: lo, Lower: lo, Upper: math.Inf(1)}
	}
	n := -(m / k) * math.Log(1-x/m)
	// Var(X) ≈ m·e^{−kn/m}(1 − (1 + kn/m)e^{−kn/m}); propagate through dn/dX = 1/(k(1 − X/m)).
	p := math.Exp(-k * n / m)
	v := m * p * (1 - (1+k*n/m)*p)
	sd := math.Sqrt(math.Max(v, 0)) / (k * (1 - x/m))
	return sketch.Estimate{Value: n, Lower: math.Max(0, n-2*sd), Upper: n + 2*sd, Empty: b.ones == 0}
}

// Merge ORs the bit arrays.
func (b *Bloom) Merge(other sketch.Sketch) error {
	o, ok := other.(*Bloom)
	if !ok || o == b || o.m != b.m || o.k != b.k {
		return sketch.ErrIncompatible
	}
	b.ones = 0
	for i := range b.set {
		b.set[i] |= o.set[i]
		b.ones += uint64(bits.OnesCount64(b.set[i]))
	}
	return nil
}

// MarshalBinary: params = bits u64, k u32; payload = words u64 × ⌈bits/64⌉.
func (b *Bloom) MarshalBinary() ([]byte, error) {
	p := &serial.Writer{}
	p.U64(b.m)
	p.U32(b.k)
	w := &serial.Writer{B: make([]byte, 0, 8*len(b.set))}
	for _, v := range b.set {
		w.U64(v)
	}
	return serial.Frame(serial.KindBloom, p.B, w.B), nil
}

func (b *Bloom) UnmarshalBinary(in []byte) error {
	params, payload, _, err := serial.Unframe(in, serial.KindBloom)
	if err != nil {
		return err
	}
	pr := serial.NewReader(params)
	m, k := pr.U64(), pr.U32()
	if pr.Err() != nil || m == 0 || m > MaxBloomBits || uint64(len(payload)) < 8*((m+63)/64) {
		return serial.ErrCorrupt
	}
	n, err := NewBloomWithParams(m, k)
	if err != nil {
		return serial.ErrCorrupt
	}
	r := serial.NewReader(payload)
	for i := range n.set {
		n.set[i] = r.U64()
		n.ones += uint64(bits.OnesCount64(n.set[i]))
	}
	if tail := m & 63; tail != 0 && n.set[len(n.set)-1]>>tail != 0 {
		return serial.ErrCorrupt // bits beyond m must be zero
	}
	*b = *n
	return nil
}
