package semantic

import "unsafe"

// MemoryUsage is a logical payload and Go-allocation estimate.  It excludes
// runtime allocator headers and map/runtime bookkeeping, so it is not a heap
// profiler result.
type MemoryUsage struct {
	Representatives uint64
	Aggregates      uint64
	Uncertainty     uint64
	Aliases         uint64
	Exemplars       uint64
	Probation       uint64
	Encoder         uint64
	Metadata        uint64
	Total           uint64
}

// MemoryUsage reports retained semantic state.  The result is deterministic
// for a snapshot and intentionally does not use process RSS.
func (s *Sketch) MemoryUsage() MemoryUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var u MemoryUsage
	u.Metadata = uint64(unsafe.Sizeof(*s))
	if s.enc != nil {
		u.Encoder = uint64(unsafe.Sizeof(*s.enc))
		// Encoder has three packed sign matrices, each one bit per padded
		// coordinate, rounded to uint64 words.
		u.Encoder += 3 * uint64((s.enc.PaddedDim()+63)/64) * 8
	}
	for _, c := range s.cs {
		u.Representatives += uint64(unsafe.Sizeof(*c)) + uint64(cap(c.id.code.Words))*8
		u.Aggregates += uint64(unsafe.Sizeof(c.agg))
		u.Uncertainty += uint64(unsafe.Sizeof(c.unc))
		u.Aliases += uint64(cap(c.id.aliases)) * 8
		u.Exemplars += uint64(cap(c.ex)) * uint64(unsafe.Sizeof(exemplar{}))
		for _, m := range c.id.members {
			u.Probation += uint64(unsafe.Sizeof(m)) + uint64(cap(m.code.Words))*8
		}
	}
	u.Total = u.Representatives + u.Aggregates + u.Uncertainty + u.Aliases + u.Exemplars + u.Probation + u.Encoder + u.Metadata
	return u
}

// MemoryBytes returns MemoryUsage().Total for callers that only need a bound.
func (s *Sketch) MemoryBytes() uint64 { return s.MemoryUsage().Total }
