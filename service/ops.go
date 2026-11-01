package service

import (
	"math"
	"strings"

	"github.com/dippendicular/cardinex/core/classic"
	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/core/sketch"
)

// Status values attached to every numeric answer.
const (
	StatusOK                     = "ok"
	StatusInsufficientConfidence = "insufficient_confidence"
)

// status is insufficient_confidence when tol > 0 and the interval is wider
// than tol.
func status(e sketch.Estimate, tol float64) string {
	if tol > 0 && (e.Upper-e.Lower > tol || math.IsInf(e.Upper, 0)) {
		return StatusInsufficientConfidence
	}
	return StatusOK
}

// Info describes a hosted sketch.
type Info struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Kind  string `json:"kind"`
	Stats any    `json:"stats"`
}

// Describe returns the entry's info and stats.
func Describe(e *Entry) Info {
	var st any
	_ = e.With(func(sk sketch.Sketch) error { st = statsOf(sk); return nil })
	return Info{ID: e.ID, Name: e.Name, Kind: kindName(e.Kind), Stats: st}
}

func statsOf(sk sketch.Sketch) any {
	switch s := sk.(type) {
	case *semantic.Sketch:
		return struct {
			Params semantic.Params `json:"params"`
			semantic.Stats
		}{s.Params(), s.Stats()}
	case *classic.HLL:
		return map[string]any{"precision": s.Precision(), "memory_bytes": s.MemoryBytes(),
			"relative_error": s.RelativeError(), "estimate": s.Estimate(), "interval": "probabilistic, about 95%"}
	case *classic.CMS:
		return map[string]any{"width": s.Width(), "depth": s.Depth(), "conservative": s.Conservative(),
			"total": s.Total(), "epsilon": s.Epsilon(), "confidence": s.Confidence(), "memory_bytes": s.MemoryBytes()}
	case *classic.Bloom:
		return map[string]any{"bits": s.Bits(), "hashes": s.Hashes(), "set_bits": s.SetBits(),
			"false_positive_rate": s.FalsePositiveRate(), "cardinality": s.EstimateCardinality(), "memory_bytes": s.MemoryBytes()}
	}
	return nil
}

// KeyAnswer is a classic-sketch query result.
type KeyAnswer struct {
	Kind     string           `json:"kind"`
	Key      string           `json:"key,omitempty"`
	Estimate *sketch.Estimate `json:"estimate,omitempty"`
	Contains *bool            `json:"contains,omitempty"`
	FPR      *float64         `json:"false_positive_rate,omitempty"`
	Interval string           `json:"interval,omitempty"`
	Status   string           `json:"status"`
}

// QueryKey answers a classic query: HLL cardinality, CMS count of key, or
// Bloom membership of key.
func QueryKey(e *Entry, key string, tol float64) (KeyAnswer, error) {
	var out KeyAnswer
	err := e.With(func(sk sketch.Sketch) error {
		out.Kind = kindName(sk.Kind())
		switch s := sk.(type) {
		case *classic.HLL:
			est := s.Estimate()
			out.Estimate, out.Interval, out.Status = &est, "probabilistic", status(est, tol)
		case *classic.CMS:
			est := s.Count([]byte(key))
			out.Key, out.Estimate, out.Interval, out.Status = key, &est, "upper guaranteed, lower probabilistic", status(est, tol)
		case *classic.Bloom:
			c, f := s.Contains([]byte(key)), s.FalsePositiveRate()
			out.Key, out.Contains, out.FPR, out.Status = key, &c, &f, StatusOK
		default:
			return ErrWrongKind
		}
		return nil
	})
	return out, err
}

// EntityView is an entity plus a confidence status.
type EntityView struct {
	semantic.Entity
	Status string `json:"status"`
}

func orderOf(by string) (semantic.Order, func(semantic.Entity) sketch.Estimate, error) {
	switch strings.ToLower(by) {
	case "", "count":
		return semantic.ByCount, func(e semantic.Entity) sketch.Estimate { return e.Count }, nil
	case "sum":
		return semantic.BySum, func(e semantic.Entity) sketch.Estimate { return e.Sum }, nil
	case "mean":
		return semantic.ByMean, func(e semantic.Entity) sketch.Estimate { return e.Mean }, nil
	}
	return 0, nil, badRequest("by must be count, sum or mean")
}

// TopKResult is a ranked entity list with sketch-level context.
type TopKResult struct {
	Entities    []EntityView    `json:"entities"`
	By          string          `json:"by"`
	W           uint64          `json:"w"`
	Wq          uint64          `json:"w_q"`
	Unmonitored sketch.Estimate `json:"unmonitored_count"`
}

func semanticOf(e *Entry, f func(*semantic.Sketch) error) error {
	return e.With(func(sk sketch.Sketch) error {
		s, ok := sk.(*semantic.Sketch)
		if !ok {
			return ErrWrongKind
		}
		return f(s)
	})
}

// TopK ranks entities; tol applies to the interval of the ranking metric.
func TopK(e *Entry, k int, by string, tol float64) (TopKResult, error) {
	order, metric, err := orderOf(by)
	if err != nil {
		return TopKResult{}, err
	}
	var out TopKResult
	err = semanticOf(e, func(s *semantic.Sketch) error {
		st := s.Stats()
		uc, _ := s.Unmonitored()
		out = TopKResult{By: strings.ToLower(orDefaultStr(by, "count")), W: st.W, Wq: st.Wq, Unmonitored: uc, Entities: []EntityView{}}
		for _, ent := range s.TopK(k, order) {
			out.Entities = append(out.Entities, EntityView{ent, status(metric(ent), tol)})
		}
		return nil
	})
	return out, err
}

// LookupResult is the entity a vector would match, if any.
type LookupResult struct {
	Found  bool        `json:"found"`
	Entity *EntityView `json:"entity,omitempty"`
	// Unmonitored is the bound that holds when no entity matched.
	Unmonitored *sketch.Estimate `json:"unmonitored_count,omitempty"`
	Status      string           `json:"status"`
}

// Lookup finds the entity for vec; metric picks the interval tol applies to.
func Lookup(e *Entry, vec []float32, metric string, tol float64) (LookupResult, error) {
	_, pick, err := orderOf(metric)
	if err != nil {
		return LookupResult{}, err
	}
	var out LookupResult
	err = semanticOf(e, func(s *semantic.Sketch) error {
		ent, ok := s.Lookup(vec)
		if !ok {
			uc, _ := s.Unmonitored()
			out = LookupResult{Unmonitored: &uc, Status: status(uc, tol)}
			return nil
		}
		v := EntityView{ent, status(pick(ent), tol)}
		out = LookupResult{Found: true, Entity: &v, Status: v.Status}
		return nil
	})
	return out, err
}

// Exemplars returns the exemplar item IDs of one entity.
func Exemplars(e *Entry, entityID uint64) ([]uint64, error) {
	var out []uint64
	err := semanticOf(e, func(s *semantic.Sketch) error {
		for _, ent := range s.TopK(0, semantic.ByCount) {
			if ent.ID == entityID {
				out = ent.Exemplars
				return nil
			}
			for _, a := range ent.Aliases {
				if a == entityID {
					out = ent.Exemplars
					return nil
				}
			}
		}
		return &Error{Code: "not_found", Msg: "entity not found"}
	})
	return out, err
}

// DeltaView is one diff row plus a status.
type DeltaView struct {
	semantic.EntityDelta
	Status string `json:"status"`
}

// Diff returns a − b per entity; tol applies to the count delta interval.
func Diff(a, b *Entry, tol float64) ([]DeltaView, error) {
	if a == b {
		return nil, badRequest("cannot diff a sketch with itself")
	}
	unlock := lockPair(a, b)
	defer unlock()
	sa, ok1 := a.sk.(*semantic.Sketch)
	sb, ok2 := b.sk.(*semantic.Sketch)
	if !ok1 || !ok2 {
		return nil, ErrWrongKind
	}
	ds, err := sa.Diff(sb)
	if err != nil {
		return nil, ErrIncompatible
	}
	out := make([]DeltaView, len(ds))
	for i, d := range ds {
		out[i] = DeltaView{d, status(d.Count, tol)}
	}
	return out, nil
}

func orDefaultStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
