package hll

import (
	"encoding/binary"
	"errors"
	"math"
	"math/bits"

	"github.com/dippendicular/cardinex/pkg/sketch"
)

const magic = "PDH1"

type HyperLogLog struct {
	Precision uint8
	registers []uint8
}

func New(precision uint8) (*HyperLogLog, error) {
	if precision < 4 || precision > 18 {
		return nil, errors.New("hll precision must be between 4 and 18")
	}
	return &HyperLogLog{Precision: precision, registers: make([]uint8, 1<<precision)}, nil
}

func (h *HyperLogLog) Add(item []byte) {
	x := sketch.Hash64WithSeed(item, 0)
	idx := x >> (64 - h.Precision)
	w := (x << h.Precision) | (1 << (h.Precision - 1))
	rank := uint8(bits.LeadingZeros64(w) + 1)
	if rank > h.registers[idx] {
		h.registers[idx] = rank
	}
}

func (h *HyperLogLog) Query(_ []byte) interface{} { return h.Count() }

func (h *HyperLogLog) Count() uint64 {
	m := float64(len(h.registers))
	var sum float64
	zeros := 0
	for _, r := range h.registers {
		sum += math.Pow(2, -float64(r))
		if r == 0 {
			zeros++
		}
	}
	raw := alpha(len(h.registers)) * m * m / sum
	if raw <= 5*m/2 && zeros > 0 {
		raw = m * math.Log(m/float64(zeros))
	}
	two64 := math.Pow(2, 64)
	if raw > two64/30 {
		if raw >= two64 {
			raw = two64
		} else {
			raw = -two64 * math.Log1p(-raw/two64)
		}
	}
	return uint64(raw + 0.5)
}

func (h *HyperLogLog) Merge(other sketch.Sketch) error {
	o, ok := other.(*HyperLogLog)
	if !ok || o.Precision != h.Precision || len(o.registers) != len(h.registers) {
		return errors.New("incompatible hll sketch")
	}
	for i := range h.registers {
		if o.registers[i] > h.registers[i] {
			h.registers[i] = o.registers[i]
		}
	}
	return nil
}

func (h *HyperLogLog) Serialize() []byte {
	out := make([]byte, 9+len(h.registers))
	copy(out, magic)
	out[4] = h.Precision
	binary.BigEndian.PutUint32(out[5:9], uint32(len(h.registers)))
	copy(out[9:], h.registers)
	return out
}

func (h *HyperLogLog) Deserialize(b []byte) error {
	if len(b) < 9 || string(b[:4]) != magic {
		return errors.New("invalid hll payload")
	}
	p := b[4]
	n := int(binary.BigEndian.Uint32(b[5:9]))
	if len(b) != 9+n || n != 1<<p {
		return errors.New("corrupt hll payload")
	}
	h.Precision = p
	h.registers = append(h.registers[:0], b[9:]...)
	return nil
}

func (h *HyperLogLog) MemoryBytes() int { return len(h.registers) }

func (h *HyperLogLog) ErrorRate() float64 { return 1.04 / math.Sqrt(float64(len(h.registers))) }

func alpha(m int) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1 + 1.079/float64(m))
	}
}
