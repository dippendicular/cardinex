package classic

import (
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

// Unmarshal decodes any classic sketch from its CDNX envelope.
func Unmarshal(b []byte) (sketch.Sketch, error) {
	k, err := serial.PeekKind(b)
	if err != nil {
		return nil, err
	}
	var s sketch.Sketch
	switch k {
	case serial.KindHLL:
		s = &HLL{}
	case serial.KindCMS:
		s = &CMS{}
	case serial.KindBloom:
		s = &Bloom{}
	default:
		return nil, serial.ErrKind
	}
	if err := s.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return s, nil
}

var (
	_ sketch.Cardinality = (*HLL)(nil)
	_ sketch.Frequency   = (*CMS)(nil)
	_ sketch.Membership  = (*Bloom)(nil)
)
