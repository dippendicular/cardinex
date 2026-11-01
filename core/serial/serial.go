// Package serial is the versioned binary envelope shared by every sketch kind.
// The layout is specified in docs/FORMAT.md:
//
//	magic "CDNX" | major u16 | minor u16 | kind u16
//	| params_len u32 | params | payload_len u32 | payload | crc32c u32
//
// Everything is little-endian; floats are IEEE-754 binary64.
package serial

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
)

const (
	Magic = "CDNX"
	Major = uint16(1)
	Minor = uint16(0)

	headerLen = 4 + 2 + 2 + 2
	// MaxSize bounds any single serialized sketch.
	MaxSize = 1 << 30
)

// Kind identifies the sketch type in the envelope.
type Kind uint16

const (
	KindHLL      Kind = 1
	KindCMS      Kind = 2
	KindBloom    Kind = 3
	KindSemantic Kind = 4
)

var (
	ErrMagic     = errors.New("serial: bad magic")
	ErrVersion   = errors.New("serial: unsupported major version")
	ErrKind      = errors.New("serial: unexpected sketch kind")
	ErrCRC       = errors.New("serial: checksum mismatch")
	ErrTruncated = errors.New("serial: truncated or oversized payload")
	ErrCorrupt   = errors.New("serial: corrupt payload")
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Frame wraps a params block and payload in the envelope.
func Frame(kind Kind, params, payload []byte) []byte {
	out := make([]byte, 0, headerLen+8+len(params)+len(payload)+4)
	out = append(out, Magic...)
	out = binary.LittleEndian.AppendUint16(out, Major)
	out = binary.LittleEndian.AppendUint16(out, Minor)
	out = binary.LittleEndian.AppendUint16(out, uint16(kind))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(params)))
	out = append(out, params...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(payload)))
	out = append(out, payload...)
	return binary.LittleEndian.AppendUint32(out, crc32.Checksum(out, castagnoli))
}

// Unframe checks the envelope and returns the params block, payload and minor
// version. Readers must ignore trailing bytes they do not understand inside
// params and payload, so newer minor versions stay readable.
func Unframe(b []byte, want Kind) (params, payload []byte, minor uint16, err error) {
	if len(b) > MaxSize || len(b) < headerLen+8+4 {
		return nil, nil, 0, ErrTruncated
	}
	body, sum := b[:len(b)-4], binary.LittleEndian.Uint32(b[len(b)-4:])
	if string(b[:4]) != Magic {
		return nil, nil, 0, ErrMagic
	}
	if crc32.Checksum(body, castagnoli) != sum {
		return nil, nil, 0, ErrCRC
	}
	r := NewReader(body[4:])
	major, minor := r.U16(), r.U16()
	if major != Major {
		return nil, nil, 0, ErrVersion
	}
	if Kind(r.U16()) != want {
		return nil, nil, 0, ErrKind
	}
	params = r.Bytes(int(r.U32()))
	payload = r.Bytes(int(r.U32()))
	if r.Err() != nil || r.Remaining() != 0 {
		return nil, nil, 0, ErrTruncated
	}
	return params, payload, minor, nil
}

// PeekKind returns the kind of an envelope after checking magic and major
// version. It does not verify the checksum.
func PeekKind(b []byte) (Kind, error) {
	if len(b) < headerLen {
		return 0, ErrTruncated
	}
	if string(b[:4]) != Magic {
		return 0, ErrMagic
	}
	r := NewReader(b[4:headerLen])
	if r.U16() != Major {
		return 0, ErrVersion
	}
	r.U16()
	return Kind(r.U16()), nil
}

// Writer appends little-endian fields.
type Writer struct{ B []byte }

func (w *Writer) U8(v uint8)    { w.B = append(w.B, v) }
func (w *Writer) U16(v uint16)  { w.B = binary.LittleEndian.AppendUint16(w.B, v) }
func (w *Writer) U32(v uint32)  { w.B = binary.LittleEndian.AppendUint32(w.B, v) }
func (w *Writer) U64(v uint64)  { w.B = binary.LittleEndian.AppendUint64(w.B, v) }
func (w *Writer) I64(v int64)   { w.U64(uint64(v)) }
func (w *Writer) F64(v float64) { w.U64(math.Float64bits(v)) }
func (w *Writer) Bool(v bool) {
	if v {
		w.U8(1)
	} else {
		w.U8(0)
	}
}

// Reader reads little-endian fields. The first failure is sticky: later reads
// return zero values and Err reports it.
type Reader struct {
	b   []byte
	off int
	err error
}

func NewReader(b []byte) *Reader { return &Reader{b: b} }

func (r *Reader) Err() error     { return r.err }
func (r *Reader) Remaining() int { return len(r.b) - r.off }

func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > r.Remaining() {
		r.err = ErrTruncated
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

// Bytes returns the next n bytes without copying.
func (r *Reader) Bytes(n int) []byte { return r.take(n) }

func (r *Reader) U8() uint8 {
	if s := r.take(1); s != nil {
		return s[0]
	}
	return 0
}

func (r *Reader) U16() uint16 {
	if s := r.take(2); s != nil {
		return binary.LittleEndian.Uint16(s)
	}
	return 0
}

func (r *Reader) U32() uint32 {
	if s := r.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}

func (r *Reader) U64() uint64 {
	if s := r.take(8); s != nil {
		return binary.LittleEndian.Uint64(s)
	}
	return 0
}

func (r *Reader) I64() int64 { return int64(r.U64()) }

func (r *Reader) F64() float64 { return math.Float64frombits(r.U64()) }

func (r *Reader) Bool() bool {
	switch r.U8() {
	case 0:
		return false
	case 1:
		return true
	}
	r.Fail()
	return false
}

// Count reads a u32 element count and fails if count*elemSize bytes cannot
// remain, which stops huge allocations from corrupt input.
func (r *Reader) Count(elemSize, max int) int {
	n := int(r.U32())
	if r.err == nil && (n > max || n*elemSize > r.Remaining()) {
		r.err = ErrCorrupt
		return 0
	}
	return n
}

// Fail marks the payload corrupt.
func (r *Reader) Fail() {
	if r.err == nil {
		r.err = ErrCorrupt
	}
}
