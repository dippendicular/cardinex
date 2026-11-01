package classic

import (
	"errors"
	"math"
	"math/bits"

	"github.com/dippendicular/cardinex/core/hashx"
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

// CMS is a Count-Min sketch (Cormode & Muthukrishnan 2005) with saturating
// uint64 counters and an optional conservative-update mode (Estan & Varghese
// 2002). Row indices use Kirsch–Mitzenmacher double hashing h1 + i·h2 from
// one XXH3-128 pass; that family is not pairwise independent, so the textbook
// ε/δ guarantee is an approximation here.
//
// Conservative-update sketches still never undercount after Merge, but the
// merged counters are the sum of the inputs, not what conservative update
// would have produced on the combined stream, so merged estimates are looser.
type CMS struct {
	w, d  uint32
	cu    bool
	c     []uint64 // row-major, d rows of w
	total uint64
}

// MaxCMSCounters bounds width·depth.
const MaxCMSCounters = 1 << 27

var errCMSShape = errors.New("classic: cms width and depth must be positive and width*depth <= 2^27")

// NewCMS returns an empty sketch.
func NewCMS(width, depth uint32, conservative bool) (*CMS, error) {
	n := uint64(width) * uint64(depth)
	if width == 0 || depth == 0 || n > MaxCMSCounters {
		return nil, errCMSShape
	}
	return &CMS{w: width, d: depth, cu: conservative, c: make([]uint64, n)}, nil
}

// NewCMSFromError sizes the sketch for additive error eps·N with probability
// 1 − delta: width = ⌈e/eps⌉, depth = ⌈ln(1/delta)⌉.
func NewCMSFromError(eps, delta float64, conservative bool) (*CMS, error) {
	if !(eps > 0 && eps < 1) || !(delta > 0 && delta < 1) {
		return nil, errors.New("classic: need 0 < eps, delta < 1")
	}
	w := math.Ceil(math.E / eps)
	d := math.Ceil(math.Log(1 / delta))
	if w*d > MaxCMSCounters {
		return nil, errCMSShape
	}
	return NewCMS(uint32(w), uint32(d), conservative)
}

func (c *CMS) Width() uint32       { return c.w }
func (c *CMS) Depth() uint32       { return c.d }
func (c *CMS) Conservative() bool  { return c.cu }
func (c *CMS) Total() uint64       { return c.total }
func (c *CMS) MemoryBytes() int    { return len(c.c) * 8 }
func (c *CMS) Epsilon() float64    { return math.E / float64(c.w) }
func (c *CMS) Confidence() float64 { return 1 - math.Exp(-float64(c.d)) }
func (c *CMS) Kind() sketch.Kind   { return sketch.KindCMS }
func (c *CMS) idx(row uint32, h1, h2 uint64) int {
	return int(row)*int(c.w) + int((h1+uint64(row)*h2)%uint64(c.w))
}

func satAdd(a, b uint64) uint64 {
	s, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return s
}

// AddKey adds n occurrences of key.
func (c *CMS) AddKey(key []byte, n uint64) {
	h1, h2 := hashx.DoubleHash(key, 0)
	c.total = satAdd(c.total, n)
	if !c.cu {
		for r := uint32(0); r < c.d; r++ {
			i := c.idx(r, h1, h2)
			c.c[i] = satAdd(c.c[i], n)
		}
		return
	}
	target := satAdd(c.min(h1, h2), n)
	for r := uint32(0); r < c.d; r++ {
		if i := c.idx(r, h1, h2); c.c[i] < target {
			c.c[i] = target
		}
	}
}

func (c *CMS) min(h1, h2 uint64) uint64 {
	m := uint64(math.MaxUint64)
	for r := uint32(0); r < c.d; r++ {
		if v := c.c[c.idx(r, h1, h2)]; v < m {
			m = v
		}
	}
	return m
}

// Count returns the estimate for key. Upper is guaranteed (Count-Min never
// undercounts). Lower = Value − ε·N holds with probability ≈ 1 − e^−depth.
func (c *CMS) Count(key []byte) sketch.Estimate {
	h1, h2 := hashx.DoubleHash(key, 0)
	v := float64(c.min(h1, h2))
	return sketch.Estimate{Value: v, Lower: math.Max(0, v-c.Epsilon()*float64(c.total)), Upper: v, Empty: c.total == 0}
}

// Merge adds counters (saturating). Both sketches must have the same shape
// and update mode.
func (c *CMS) Merge(other sketch.Sketch) error {
	o, ok := other.(*CMS)
	if !ok || o == c || o.w != c.w || o.d != c.d || o.cu != c.cu {
		return sketch.ErrIncompatible
	}
	for i := range c.c {
		c.c[i] = satAdd(c.c[i], o.c[i])
	}
	c.total = satAdd(c.total, o.total)
	return nil
}

// MarshalBinary: params = width u32, depth u32, conservative u8;
// payload = total u64, counters u64 × width·depth.
func (c *CMS) MarshalBinary() ([]byte, error) {
	p := &serial.Writer{}
	p.U32(c.w)
	p.U32(c.d)
	p.Bool(c.cu)
	w := &serial.Writer{B: make([]byte, 0, 8+8*len(c.c))}
	w.U64(c.total)
	for _, v := range c.c {
		w.U64(v)
	}
	return serial.Frame(serial.KindCMS, p.B, w.B), nil
}

func (c *CMS) UnmarshalBinary(b []byte) error {
	params, payload, _, err := serial.Unframe(b, serial.KindCMS)
	if err != nil {
		return err
	}
	pr := serial.NewReader(params)
	w, d, cu := pr.U32(), pr.U32(), pr.Bool()
	if pr.Err() != nil {
		return serial.ErrCorrupt
	}
	n, err := NewCMSIfFits(w, d, cu, len(payload))
	if err != nil {
		return err
	}
	r := serial.NewReader(payload)
	n.total = r.U64()
	for i := range n.c {
		n.c[i] = r.U64()
	}
	if r.Err() != nil {
		return serial.ErrCorrupt
	}
	*c = *n
	return nil
}

// NewCMSIfFits checks the shape against the payload size before allocating.
func NewCMSIfFits(w, d uint32, cu bool, payloadLen int) (*CMS, error) {
	n := uint64(w) * uint64(d)
	if w == 0 || d == 0 || n > MaxCMSCounters || uint64(payloadLen) < 8+8*n {
		return nil, serial.ErrCorrupt
	}
	return NewCMS(w, d, cu)
}
