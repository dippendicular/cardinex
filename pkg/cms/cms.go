package cms

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/dippendicular/cardinex/pkg/sketch"
)

const magic = "PDC1"

type CountMinSketch struct {
	Width uint32
	Depth uint32
	rows  [][]uint64
	total uint64
}

func New(width, depth uint32) (*CountMinSketch, error) {
	if width == 0 || depth == 0 {
		return nil, errors.New("cms width and depth must be positive")
	}
	c := &CountMinSketch{Width: width, Depth: depth, rows: make([][]uint64, depth)}
	for i := range c.rows {
		c.rows[i] = make([]uint64, width)
	}
	return c, nil
}

func FromError(epsilon, confidence float64) (*CountMinSketch, error) {
	if epsilon <= 0 || confidence <= 0 || confidence >= 1 {
		return nil, errors.New("invalid cms error/confidence")
	}
	w := uint32(math.Ceil(math.E / epsilon))
	d := uint32(math.Ceil(math.Log(1/(1-confidence)) / math.Log(math.E)))
	return New(w, d)
}

func (c *CountMinSketch) Add(item []byte) { c.AddN(item, 1) }

func (c *CountMinSketch) AddN(item []byte, n uint64) {
	h1, h2 := sketch.DoubleHash(item)
	for i := uint32(0); i < c.Depth; i++ {
		idx := (h1 + uint64(i)*h2) % uint64(c.Width)
		c.rows[i][idx] += n
	}
	c.total += n
}

func (c *CountMinSketch) Query(item []byte) interface{} { return c.Estimate(item) }

func (c *CountMinSketch) Estimate(item []byte) uint64 {
	h1, h2 := sketch.DoubleHash(item)
	min := ^uint64(0)
	for i := uint32(0); i < c.Depth; i++ {
		idx := (h1 + uint64(i)*h2) % uint64(c.Width)
		if c.rows[i][idx] < min {
			min = c.rows[i][idx]
		}
	}
	return min
}

func (c *CountMinSketch) Merge(other sketch.Sketch) error {
	o, ok := other.(*CountMinSketch)
	if !ok || o.Width != c.Width || o.Depth != c.Depth {
		return errors.New("incompatible cms sketch")
	}
	for i := range c.rows {
		for j := range c.rows[i] {
			c.rows[i][j] += o.rows[i][j]
		}
	}
	c.total += o.total
	return nil
}

func (c *CountMinSketch) Serialize() []byte {
	out := make([]byte, 20+int(c.Width*c.Depth)*8)
	copy(out, magic)
	binary.BigEndian.PutUint32(out[4:8], c.Width)
	binary.BigEndian.PutUint32(out[8:12], c.Depth)
	binary.BigEndian.PutUint64(out[12:20], c.total)
	off := 20
	for i := range c.rows {
		for _, v := range c.rows[i] {
			binary.BigEndian.PutUint64(out[off:off+8], v)
			off += 8
		}
	}
	return out
}

func (c *CountMinSketch) Deserialize(b []byte) error {
	if len(b) < 20 || string(b[:4]) != magic {
		return errors.New("invalid cms payload")
	}
	w := binary.BigEndian.Uint32(b[4:8])
	d := binary.BigEndian.Uint32(b[8:12])
	want := 20 + int(w*d)*8
	if w == 0 || d == 0 || len(b) != want {
		return errors.New("corrupt cms payload")
	}
	nc, _ := New(w, d)
	nc.total = binary.BigEndian.Uint64(b[12:20])
	off := 20
	for i := range nc.rows {
		for j := range nc.rows[i] {
			nc.rows[i][j] = binary.BigEndian.Uint64(b[off : off+8])
			off += 8
		}
	}
	*c = *nc
	return nil
}

func (c *CountMinSketch) Total() uint64       { return c.total }
func (c *CountMinSketch) MemoryBytes() int    { return int(c.Width*c.Depth) * 8 }
func (c *CountMinSketch) Epsilon() float64    { return math.E / float64(c.Width) }
func (c *CountMinSketch) Confidence() float64 { return 1 - math.Pow(1/math.E, float64(c.Depth)) }
