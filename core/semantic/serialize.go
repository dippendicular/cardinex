package semantic

import (
	"math"

	"github.com/dippendicular/cardinex/core/quant"
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

// Kind is the envelope kind for this sketch.
func (s *Sketch) Kind() sketch.Kind { return sketch.KindSemantic }

var _ sketch.Sketch = (*Sketch)(nil)

// MarshalBinary encodes the sketch in the docs/FORMAT.md layout.
func (s *Sketch) MarshalBinary() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return serial.Frame(serial.KindSemantic, encodeParams(s.p), s.encodePayload()), nil
}

// Unmarshal decodes a sketch produced by MarshalBinary.
func Unmarshal(b []byte) (*Sketch, error) {
	s := &Sketch{seq: instanceSeq.Add(1)}
	if err := s.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return s, nil
}

// UnmarshalBinary replaces the sketch's state. On error the sketch is
// unchanged.
func (s *Sketch) UnmarshalBinary(b []byte) error {
	params, payload, _, err := serial.Unframe(b, serial.KindSemantic)
	if err != nil {
		return err
	}
	p, err := decodeParams(params)
	if err != nil {
		return err
	}
	n, err := New(p)
	if err != nil {
		return err
	}
	if err := n.decodePayload(payload); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seq == 0 {
		s.seq = instanceSeq.Add(1)
	}
	s.p, s.enc, s.cs = n.p, n.enc, n.cs
	s.floor, s.w, s.wSum, s.wq, s.wqSum = n.floor, n.w, n.wSum, n.wq, n.wqSum
	s.orphaned, s.rejected = n.orphaned, n.rejected
	s.promoted, s.reAnchored, s.dissolved = n.promoted, n.reAnchored, n.dissolved
	s.fused, s.aliased, s.dropped = n.fused, n.aliased, n.dropped
	return nil
}

func encodeParams(p Params) []byte {
	w := &serial.Writer{}
	w.U32(uint32(p.Dim))
	w.U32(uint32(p.Capacity))
	w.F64(p.Tau)
	w.F64(p.Band)
	w.U8(p.Bits)
	w.F64(p.ValueMin)
	w.F64(p.ValueMax)
	w.U16(uint16(p.Exemplars))
	w.U32(uint32(p.Probation))
	w.Bool(p.Refine)
	w.U64(p.Seed)
	return w.B
}

// maxDim and maxCapacity bound what a payload may ask us to allocate.
const (
	maxDim      = 1 << 16
	maxCapacity = 1 << 22
)

func decodeParams(b []byte) (Params, error) {
	r := serial.NewReader(b)
	p := Params{
		Dim:       int(r.U32()),
		Capacity:  int(r.U32()),
		Tau:       r.F64(),
		Band:      r.F64(),
		Bits:      r.U8(),
		ValueMin:  r.F64(),
		ValueMax:  r.F64(),
		Exemplars: int(r.U16()),
		Probation: int(r.U32()),
		Refine:    r.Bool(),
		Seed:      r.U64(),
	}
	if r.Err() != nil {
		return Params{}, r.Err()
	}
	if p.Dim > maxDim || p.Capacity > maxCapacity {
		return Params{}, serial.ErrCorrupt
	}
	return p, nil
}

func writeKahan(w *serial.Writer, k kahan) {
	w.F64(k.sum)
	w.F64(k.c)
}

func writeCode(w *serial.Writer, c quant.Code) {
	w.U32(uint32(len(c.Words)))
	for _, x := range c.Words {
		w.U64(x)
	}
	w.I64(c.Norm2)
}

func (s *Sketch) encodePayload() []byte {
	w := &serial.Writer{}
	w.U64(s.floor)
	w.U64(s.w)
	writeKahan(w, s.wSum)
	w.U64(s.wq)
	writeKahan(w, s.wqSum)
	w.U64(s.orphaned)
	for _, v := range []uint64{s.rejected.Dim, s.rejected.NonFinite, s.rejected.ZeroNorm, s.rejected.Value,
		s.promoted, s.reAnchored, s.dissolved, s.fused, s.aliased, s.dropped} {
		w.U64(v)
	}
	w.U32(uint32(len(s.cs)))
	for _, c := range s.cs {
		// Aggregation
		w.U64(c.agg.g)
		writeKahan(w, c.agg.G)
		w.U64(c.agg.inh)
		w.U64(c.agg.floorAdd)
		w.F64(c.agg.inhSum)
		// Identity
		w.U64(c.id.id)
		writeCode(w, c.id.code)
		w.Bool(c.id.provisional)
		w.U32(c.id.confident)
		w.U64(c.id.seedItem)
		w.F64(c.id.seedValue)
		w.Bool(c.id.seedLive)
		w.U32(uint32(len(c.id.aliases)))
		for _, a := range c.id.aliases {
			w.U64(a)
		}
		w.U32(uint32(len(c.id.members)))
		for _, m := range c.id.members {
			w.U64(m.item)
			writeCode(w, m.code)
		}
		// Uncertainty
		w.U64(c.unc.count)
		writeKahan(w, c.unc.sum)
		// Exemplars
		w.U32(uint32(len(c.ex)))
		for _, e := range c.ex {
			w.U64(e.prio)
			w.U64(e.item)
		}
	}
	return w.B
}

func finite(xs ...float64) bool {
	for _, x := range xs {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

func readKahan(r *serial.Reader) kahan {
	k := kahan{sum: r.F64(), c: r.F64()}
	if !finite(k.sum, k.c) {
		r.Fail()
	}
	return k
}

func (s *Sketch) readCode(r *serial.Reader) quant.Code {
	want := codeWords(s.enc)
	n := r.Count(8, want)
	if n != want {
		r.Fail()
		return quant.Code{}
	}
	c := quant.Code{Words: make([]uint64, n)}
	for i := range c.Words {
		c.Words[i] = r.U64()
	}
	c.Norm2 = r.I64()
	if (s.p.Bits == 1) != (c.Norm2 == 0) || c.Norm2 < 0 {
		r.Fail()
	}
	return c
}

func codeWords(e *quant.Encoder) int {
	per := 64 / int(e.Bits())
	return (e.PaddedDim() + per - 1) / per
}

// decodePayload fills a fresh sketch from New(p).
func (s *Sketch) decodePayload(b []byte) error {
	r := serial.NewReader(b)
	s.floor = r.U64()
	s.w = r.U64()
	s.wSum = readKahan(r)
	s.wq = r.U64()
	s.wqSum = readKahan(r)
	s.orphaned = r.U64()
	s.rejected = Rejections{Dim: r.U64(), NonFinite: r.U64(), ZeroNorm: r.U64(), Value: r.U64()}
	s.promoted, s.reAnchored, s.dissolved = r.U64(), r.U64(), r.U64()
	s.fused, s.aliased, s.dropped = r.U64(), r.U64(), r.U64()

	n := r.Count(64, s.p.Capacity)
	ids := map[uint64]bool{}
	s.cs = make([]*counter, 0, n)
	for i := 0; i < n && r.Err() == nil; i++ {
		c := &counter{}
		c.agg.g = r.U64()
		c.agg.G = readKahan(r)
		c.agg.inh = r.U64()
		c.agg.floorAdd = r.U64()
		c.agg.inhSum = r.F64()

		c.id.id = r.U64()
		c.id.code = s.readCode(r)
		c.id.provisional = r.Bool()
		c.id.confident = r.U32()
		c.id.seedItem = r.U64()
		c.id.seedValue = r.F64()
		c.id.seedLive = r.Bool()
		na := r.Count(8, maxCapacity)
		for j := 0; j < na; j++ {
			c.id.aliases = append(c.id.aliases, r.U64())
		}
		nm := r.Count(16, s.p.Probation)
		for j := 0; j < nm; j++ {
			item := r.U64()
			c.id.members = append(c.id.members, member{s.readCode(r), item})
		}

		c.unc.count = r.U64()
		c.unc.sum = readKahan(r)

		ne := r.Count(16, s.p.Exemplars)
		for j := 0; j < ne; j++ {
			c.ex = append(c.ex, exemplar{r.U64(), r.U64()})
		}

		if !finite(c.agg.inhSum, c.id.seedValue) || ids[c.id.id] || (!c.id.provisional && len(c.id.members) > 0) {
			r.Fail()
		}
		for j := 1; j < len(c.ex); j++ {
			if !exLess(c.ex[j-1], c.ex[j]) {
				r.Fail()
			}
		}
		ids[c.id.id] = true
		s.cs = append(s.cs, c)
	}
	// Trailing bytes are allowed: they belong to a newer minor version.
	return r.Err()
}
