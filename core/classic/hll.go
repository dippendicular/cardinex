// Package classic holds the exact-key building blocks: HyperLogLog,
// Count-Min and Bloom. They hash with XXH3 (core/hashx) and serialize to the
// CDNX envelope (docs/FORMAT.md).
package classic

import (
	"errors"
	"math"
	"math/bits"

	"github.com/dippendicular/cardinex/core/hashx"
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

// HLL is a dense HyperLogLog with 6-bit packed registers and Ertl's improved
// estimator ("New cardinality estimation algorithms for HyperLogLog
// sketches", Ertl 2017). The improved estimator is unbiased over the whole
// range without the HLL++ empirical bias tables (Heule et al. 2013) and needs
// neither linear counting nor the 32-bit large-range correction.
type HLL struct {
	p    uint8
	regs []byte // 6 bits per register, little-endian bit order, +1 pad byte
}

const (
	hllMinP = 4
	hllMaxP = 18
)

var errHLLPrecision = errors.New("classic: hll precision must be in [4, 18]")

// NewHLL returns an empty HyperLogLog with 2^p registers.
func NewHLL(p uint8) (*HLL, error) {
	if p < hllMinP || p > hllMaxP {
		return nil, errHLLPrecision
	}
	m := 1 << p
	return &HLL{p: p, regs: make([]byte, (m*6+7)/8+1)}, nil
}

func (h *HLL) Precision() uint8 { return h.p }
func (h *HLL) m() int           { return 1 << h.p }
func (h *HLL) maxRank() uint8   { return 65 - h.p }

func (h *HLL) get(i int) uint8 {
	off := i * 6
	w := uint16(h.regs[off>>3]) | uint16(h.regs[off>>3+1])<<8
	return uint8(w>>(off&7)) & 0x3f
}

func (h *HLL) set(i int, v uint8) {
	off := i * 6
	w := uint16(h.regs[off>>3]) | uint16(h.regs[off>>3+1])<<8
	w = w&^(0x3f<<(off&7)) | uint16(v)<<(off&7)
	h.regs[off>>3], h.regs[off>>3+1] = byte(w), byte(w>>8)
}

// AddKey inserts key.
func (h *HLL) AddKey(key []byte) {
	x := hashx.Hash64(key, 0)
	idx := int(x >> (64 - h.p))
	rank := uint8(bits.LeadingZeros64(x<<h.p)) + 1
	if r := h.maxRank(); rank > r {
		rank = r
	}
	if rank > h.get(idx) {
		h.set(idx, rank)
	}
}

// Estimate returns the cardinality estimate. The interval is ±2 relative
// standard errors (1.04/√m), about 95%; it is probabilistic, not guaranteed.
func (h *HLL) Estimate() sketch.Estimate {
	m := h.m()
	q := int(64 - h.p)
	counts := make([]float64, q+2)
	for i := 0; i < m; i++ {
		counts[h.get(i)]++
	}
	fm := float64(m)
	z := fm * ertlTau(1-counts[q+1]/fm)
	for k := q; k >= 1; k-- {
		z = 0.5 * (z + counts[k])
	}
	z += fm * ertlSigma(counts[0]/fm)
	est := fm * fm / (2 * math.Ln2) / z
	if math.IsInf(est, 0) || math.IsNaN(est) {
		est = 0
	}
	rse := 1.04 / math.Sqrt(fm)
	return sketch.Estimate{Value: est, Lower: math.Max(0, est*(1-2*rse)), Upper: est * (1 + 2*rse), Empty: counts[0] == fm}
}

func ertlSigma(x float64) float64 {
	if x == 1 {
		return math.Inf(1)
	}
	y, z := 1.0, x
	for {
		x *= x
		prev := z
		z += x * y
		y += y
		if prev == z {
			return z
		}
	}
}

func ertlTau(x float64) float64 {
	if x == 0 || x == 1 {
		return 0
	}
	y, z := 1.0, 1-x
	for {
		x = math.Sqrt(x)
		prev := z
		y *= 0.5
		z -= (1 - x) * (1 - x) * y
		if prev == z {
			return z / 3
		}
	}
}

// RelativeError is 1.04/√m.
func (h *HLL) RelativeError() float64 { return 1.04 / math.Sqrt(float64(h.m())) }

// MemoryBytes is the register storage size.
func (h *HLL) MemoryBytes() int { return len(h.regs) }

func (h *HLL) Kind() sketch.Kind { return sketch.KindHLL }

// Merge takes the register-wise maximum.
func (h *HLL) Merge(other sketch.Sketch) error {
	o, ok := other.(*HLL)
	if !ok || o.p != h.p || o == h {
		return sketch.ErrIncompatible
	}
	for i := 0; i < h.m(); i++ {
		if v := o.get(i); v > h.get(i) {
			h.set(i, v)
		}
	}
	return nil
}

// MarshalBinary: params = p u8; payload = packed registers.
func (h *HLL) MarshalBinary() ([]byte, error) {
	return serial.Frame(serial.KindHLL, []byte{h.p}, h.regs[:len(h.regs)-1]), nil
}

func (h *HLL) UnmarshalBinary(b []byte) error {
	params, payload, _, err := serial.Unframe(b, serial.KindHLL)
	if err != nil {
		return err
	}
	if len(params) < 1 {
		return serial.ErrCorrupt
	}
	n, err := NewHLL(params[0])
	if err != nil {
		return serial.ErrCorrupt
	}
	if len(payload) < len(n.regs)-1 {
		return serial.ErrCorrupt
	}
	copy(n.regs, payload[:len(n.regs)-1])
	for i := 0; i < n.m(); i++ {
		if n.get(i) > n.maxRank() {
			return serial.ErrCorrupt
		}
	}
	*h = *n
	return nil
}
