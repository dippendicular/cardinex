// Package cardinex is the pure-Go entry point (no cgo): thin re-exports of
// the core packages.
//
//	s, _ := cardinex.NewSemantic(cardinex.DefaultSemanticParams(384, 0.6, 1))
//	_ = s.Add(vec, 0.7, 42)
package cardinex

import (
	"encoding/json"

	"github.com/dippendicular/cardinex/core/build"
	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/core/sketch"
)

type (
	Sketch              = sketch.Sketch
	Estimate            = sketch.Estimate
	Semantic            = semantic.Sketch
	SemanticParams      = semantic.Params
	SemanticMemoryUsage = semantic.MemoryUsage
	Entity              = semantic.Entity
	EntityDelta         = semantic.EntityDelta
	Order               = semantic.Order
	HLL                 = classic.HLL
	CMS                 = classic.CMS
	Bloom               = classic.Bloom
)

const (
	ByCount = semantic.ByCount
	BySum   = semantic.BySum
	ByMean  = semantic.ByMean
)

func DefaultSemanticParams(dim int, tau float64, seed uint64) SemanticParams {
	return semantic.DefaultParams(dim, tau, seed)
}

func NewSemantic(p SemanticParams) (*Semantic, error) { return semantic.New(p) }
func NewHLL(precision uint8) (*HLL, error)            { return classic.NewHLL(precision) }
func NewBloom(n int, fpr float64) (*Bloom, error)     { return classic.NewBloom(n, fpr) }
func NewCMS(width, depth uint32, conservative bool) (*CMS, error) {
	return classic.NewCMS(width, depth, conservative)
}

// New builds any sketch from a kind name and JSON params (the HTTP/C ABI shape).
func New(kind string, params json.RawMessage) (Sketch, error) { return build.New(kind, params) }

// Decode reads any CDNX envelope.
func Decode(b []byte) (Sketch, error) { return build.Decode(b) }

// TauFor picks τ satisfying Δ > 4ρ + 4η + 2·band.
func TauFor(rho, delta, eta, band float64) (float64, error) {
	return semantic.TauFor(rho, delta, eta, band)
}
