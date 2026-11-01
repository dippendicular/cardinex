package conformance

import (
	"encoding/json"
	"fmt"

	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/core/sketch"
)

// Spec is conformance/spec.json.
type Spec struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

// Case is one conformance case. Params use the same JSON shape as the HTTP
// API and the SDK constructors.
type Case struct {
	Name    string          `json:"name"`
	Kind    string          `json:"kind"`
	Params  json.RawMessage `json:"params"`
	Keys    *KeyOps         `json:"keys,omitempty"`
	Counts  uint64          `json:"count_per_key,omitempty"` // CMS: n per AddKey
	Vectors *VectorOps      `json:"vectors,omitempty"`
	Golden  string          `json:"golden"`
	Expect  Expect          `json:"expect"`
}

// Expect lists answers to check after decoding the golden file. Floats are
// compared with an absolute tolerance of 1e-9 unless stated otherwise.
type Expect struct {
	Estimate   *sketch.Estimate  `json:"estimate,omitempty"`    // HLL
	Counts     map[string]uint64 `json:"counts,omitempty"`      // CMS: exact Value
	Contains   map[string]bool   `json:"contains,omitempty"`    // Bloom
	SetBits    uint64            `json:"set_bits,omitempty"`    // Bloom
	TopK       []semantic.Entity `json:"topk,omitempty"`        // semantic, by count, all entities
	StatsW     uint64            `json:"stats_w,omitempty"`     // semantic
	StatsLive  int               `json:"stats_live,omitempty"`  // semantic
	StatsFloor uint64            `json:"stats_floor,omitempty"` // semantic
}

// DefaultCases is the case list the generator writes.
var DefaultCases = []Case{
	{Name: "hll_p12", Kind: "hll", Params: raw(`{"precision":12}`), Keys: &KeyOps{Prefix: "k", Count: 20000}},
	{Name: "cms_1024x4", Kind: "cms", Params: raw(`{"width":1024,"depth":4}`), Keys: &KeyOps{Prefix: "k", Count: 5000, Modulo: 300}, Counts: 2},
	{Name: "cms_cu_256x3", Kind: "cms", Params: raw(`{"width":256,"depth":3,"conservative":true}`), Keys: &KeyOps{Prefix: "k", Count: 5000, Modulo: 700}, Counts: 1},
	{Name: "bloom_8192x5", Kind: "bloom", Params: raw(`{"bits":8192,"hashes":5}`), Keys: &KeyOps{Prefix: "k", Count: 600}},
	{Name: "semantic_d16", Kind: "semantic",
		Params:  raw(`{"dim":16,"tau":0.5,"band":0.05,"seed":7,"capacity":4,"probation":2,"exemplars":4}`),
		Vectors: &VectorOps{Dim: 16, Entities: 6, Items: 120}},
	{Name: "semantic_d40_bits2", Kind: "semantic",
		Params:  raw(`{"dim":40,"tau":0.6,"band":0.05,"seed":11,"capacity":8,"bits":2,"probation":0,"exemplars":3}`),
		Vectors: &VectorOps{Dim: 40, Entities: 10, Items: 200}},
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

// BuildFunc constructs a sketch from kind and params. The service's Build is
// used so the JSON shape is identical everywhere.
type BuildFunc func(kind string, params json.RawMessage) (sketch.Sketch, error)

// Run builds the case's sketch by replaying its inputs.
func Run(c Case, build BuildFunc) (sketch.Sketch, error) {
	sk, err := build(c.Kind, c.Params)
	if err != nil {
		return nil, err
	}
	switch s := sk.(type) {
	case *classic.CMS:
		n := c.Counts
		if n == 0 {
			n = 1
		}
		for _, k := range c.Keys.Keys() {
			s.AddKey(k, n)
		}
	case sketch.Cardinality:
		for _, k := range c.Keys.Keys() {
			s.AddKey(k)
		}
	case sketch.Membership:
		for _, k := range c.Keys.Keys() {
			s.AddKey(k)
		}
	case *semantic.Sketch:
		for i := 0; i < c.Vectors.Items; i++ {
			if err := s.Add(c.Vectors.Vector(i), Value(i), ID(i)); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", c.Kind)
	}
	return sk, nil
}

// Answers computes Expect from a sketch.
func Answers(c Case, sk sketch.Sketch) Expect {
	var e Expect
	switch s := sk.(type) {
	case *classic.HLL:
		est := s.Estimate()
		e.Estimate = &est
	case *classic.CMS:
		e.Counts = map[string]uint64{}
		for _, k := range []string{"k0", "k1", "k42", "k299", "absent"} {
			e.Counts[k] = uint64(s.Count([]byte(k)).Value)
		}
	case *classic.Bloom:
		e.Contains = map[string]bool{}
		for _, k := range []string{"k0", "k1", "k599", "absent-1", "absent-2", "absent-3"} {
			e.Contains[k] = s.Contains([]byte(k))
		}
		e.SetBits = s.SetBits()
	case *semantic.Sketch:
		e.TopK = s.TopK(0, semantic.ByCount)
		st := s.Stats()
		e.StatsW, e.StatsLive, e.StatsFloor = st.W, st.Live, st.Floor
	}
	return e
}
