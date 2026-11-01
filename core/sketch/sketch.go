// Package sketch defines the typed capability interfaces every Cardinex
// sketch implements (IMPLEMENTATION.md §2).
package sketch

import (
	"errors"

	"github.com/dippendicular/cardinex/core/serial"
)

// Kind identifies a sketch type; it is the CDNX envelope kind.
type Kind = serial.Kind

const (
	KindHLL      = serial.KindHLL
	KindCMS      = serial.KindCMS
	KindBloom    = serial.KindBloom
	KindSemantic = serial.KindSemantic
)

// ErrIncompatible is returned by Merge when parameters differ.
var ErrIncompatible = errors.New("cardinex: incompatible sketches")

// Estimate is a point value with an interval. Each sketch documents whether
// the interval is guaranteed or probabilistic. Empty is set when there is no
// data behind the value.
type Estimate struct {
	Value float64 `json:"value"`
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
	Empty bool    `json:"empty,omitempty"`
}

// Sketch is implemented by every sketch kind.
type Sketch interface {
	Kind() Kind
	Merge(other Sketch) error
	MarshalBinary() ([]byte, error)
	UnmarshalBinary([]byte) error
}

// Cardinality estimates the number of distinct keys (HyperLogLog).
type Cardinality interface {
	Sketch
	AddKey(key []byte)
	Estimate() Estimate
}

// Frequency estimates per-key counts (Count-Min).
type Frequency interface {
	Sketch
	AddKey(key []byte, n uint64)
	Count(key []byte) Estimate
}

// Membership answers set membership with one-sided error (Bloom).
type Membership interface {
	Sketch
	AddKey(key []byte)
	Contains(key []byte) bool
}

// PeekKind reads the kind from a CDNX envelope without decoding it.
func PeekKind(b []byte) (Kind, error) { return serial.PeekKind(b) }
