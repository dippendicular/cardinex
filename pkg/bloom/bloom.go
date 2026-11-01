package bloom

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/dippendicular/cardinex/pkg/sketch"
)

const magic = "PDB1"

type BloomFilter struct {
	Bits uint64
	K    uint32
	set  []byte
	n    uint64
}

func New(expectedElements int, falsePositiveRate float64) (*BloomFilter, error) {
	if expectedElements <= 0 || falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		return nil, errors.New("invalid bloom parameters")
	}
	m := uint64(math.Ceil(-float64(expectedElements) * math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2)))
	k := uint32(math.Ceil((float64(m) / float64(expectedElements)) * math.Ln2))
	return NewWithParams(m, k)
}

func NewWithParams(bits uint64, k uint32) (*BloomFilter, error) {
	if bits == 0 || k == 0 {
		return nil, errors.New("bloom bits and hash count must be positive")
	}
	return &BloomFilter{Bits: bits, K: k, set: make([]byte, (bits+7)/8)}, nil
}

func (b *BloomFilter) Add(item []byte) {
	h1, h2 := sketch.DoubleHash(item)
	for i := uint32(0); i < b.K; i++ {
		pos := (h1 + uint64(i)*h2) % b.Bits
		b.set[pos/8] |= 1 << (pos % 8)
	}
	b.n++
}

func (b *BloomFilter) Query(item []byte) interface{} { return b.Contains(item) }

func (b *BloomFilter) Contains(item []byte) bool {
	h1, h2 := sketch.DoubleHash(item)
	for i := uint32(0); i < b.K; i++ {
		pos := (h1 + uint64(i)*h2) % b.Bits
		if b.set[pos/8]&(1<<(pos%8)) == 0 {
			return false
		}
	}
	return true
}

func (b *BloomFilter) Merge(other sketch.Sketch) error {
	o, ok := other.(*BloomFilter)
	if !ok || o.Bits != b.Bits || o.K != b.K || len(o.set) != len(b.set) {
		return errors.New("incompatible bloom filter")
	}
	for i := range b.set {
		b.set[i] |= o.set[i]
	}
	b.n += o.n
	return nil
}

func (b *BloomFilter) Serialize() []byte {
	out := make([]byte, 24+len(b.set))
	copy(out, magic)
	binary.BigEndian.PutUint64(out[4:12], b.Bits)
	binary.BigEndian.PutUint32(out[12:16], b.K)
	binary.BigEndian.PutUint64(out[16:24], b.n)
	copy(out[24:], b.set)
	return out
}

func (b *BloomFilter) Deserialize(in []byte) error {
	if len(in) < 24 || string(in[:4]) != magic {
		return errors.New("invalid bloom payload")
	}
	bits := binary.BigEndian.Uint64(in[4:12])
	k := binary.BigEndian.Uint32(in[12:16])
	want := 24 + int((bits+7)/8)
	if bits == 0 || k == 0 || len(in) != want {
		return errors.New("corrupt bloom payload")
	}
	b.Bits = bits
	b.K = k
	b.n = binary.BigEndian.Uint64(in[16:24])
	b.set = append(b.set[:0], in[24:]...)
	return nil
}

func (b *BloomFilter) MemoryBytes() int { return len(b.set) }
func (b *BloomFilter) Inserts() uint64  { return b.n }
func (b *BloomFilter) FalsePositiveRate() float64 {
	return math.Pow(1-math.Exp(-float64(b.K)*float64(b.n)/float64(b.Bits)), float64(b.K))
}
