// Package build constructs sketches by kind name from JSON params, and
// decodes any CDNX envelope.
package build

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/core/serial"
	"github.com/dippendicular/cardinex/core/sketch"
)

// classicParams covers every classic constructor; unused fields are ignored.
type classicParams struct {
	Precision         uint8   `json:"precision"`
	RelativeError     float64 `json:"relative_error"`
	Width             uint32  `json:"width"`
	Depth             uint32  `json:"depth"`
	Epsilon           float64 `json:"epsilon"`
	Delta             float64 `json:"delta"`
	Conservative      bool    `json:"conservative"`
	ExpectedElements  int     `json:"expected_elements"`
	FalsePositiveRate float64 `json:"false_positive_rate"`
	Bits              uint64  `json:"bits"`
	Hashes            uint32  `json:"hashes"`
}

// New constructs a sketch of the named kind (semantic, hll, cms, bloom)
// from JSON params. This JSON shape is shared by the HTTP API, the MCP
// server, the C ABI and the conformance suite.
func New(kind string, raw json.RawMessage) (sketch.Sketch, error) {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := func(v any) error {
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.DisallowUnknownFields()
		if err := d.Decode(v); err != nil {
			return fmt.Errorf("params: %v", err)
		}
		return nil
	}
	var (
		sk  sketch.Sketch
		err error
	)
	switch strings.ToLower(kind) {
	case "semantic":
		var probe struct {
			Dim  int     `json:"dim"`
			Tau  float64 `json:"tau"`
			Seed uint64  `json:"seed"`
		}
		_ = json.Unmarshal(raw, &probe)
		p := semantic.DefaultParams(probe.Dim, probe.Tau, probe.Seed)
		if err := dec(&p); err != nil {
			return nil, err
		}
		sk, err = semantic.New(p)
	case "hll", "hyperloglog":
		var p classicParams
		if err := dec(&p); err != nil {
			return nil, err
		}
		if p.Precision == 0 {
			p.Precision = 14
			if p.RelativeError > 0 {
				p.Precision = uint8(min(18, max(4, int(2*math.Log2(1.04/p.RelativeError))+1)))
			}
		}
		sk, err = classic.NewHLL(p.Precision)
	case "cms", "countmin":
		var p classicParams
		if err := dec(&p); err != nil {
			return nil, err
		}
		if p.Epsilon > 0 {
			sk, err = classic.NewCMSFromError(p.Epsilon, orDefault(p.Delta, 0.01), p.Conservative)
		} else {
			sk, err = classic.NewCMS(orDefaultU32(p.Width, 2720), orDefaultU32(p.Depth, 5), p.Conservative)
		}
	case "bloom":
		var p classicParams
		if err := dec(&p); err != nil {
			return nil, err
		}
		if p.Bits > 0 {
			sk, err = classic.NewBloomWithParams(p.Bits, orDefaultU32(p.Hashes, 7))
		} else {
			sk, err = classic.NewBloom(orDefaultInt(p.ExpectedElements, 100000), orDefault(p.FalsePositiveRate, 0.01))
		}
	default:
		return nil, fmt.Errorf("unknown sketch kind %q", kind)
	}
	return sk, err
}

// Decode turns CDNX bytes into a sketch of any kind.
func Decode(b []byte) (sketch.Sketch, error) {
	k, err := serial.PeekKind(b)
	if err != nil {
		return nil, err
	}
	if k == serial.KindSemantic {
		return semantic.Unmarshal(b)
	}
	return classic.Unmarshal(b)
}

func orDefault(v, d float64) float64 {
	if v == 0 {
		return d
	}
	return v
}

func orDefaultU32(v, d uint32) uint32 {
	if v == 0 {
		return d
	}
	return v
}

func orDefaultInt(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}
