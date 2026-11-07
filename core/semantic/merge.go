package semantic

import (
	"fmt"
	"math"
	"sort"

	"github.com/dippendicular/cardinex/core/quant"
	"github.com/dippendicular/cardinex/core/sketch"
)

// maxAliasesPerCounter bounds historical identity links retained by a
// representative.  Aliases are hints for callers, not geometric evidence.
const maxAliasesPerCounter = 64

// lockPair locks two distinct sketches in sequence order (SPEC §8).
func lockPair(a, b *Sketch) func() {
	first, second := a, b
	if b.seq < a.seq {
		first, second = b, a
	}
	first.mu.Lock()
	second.mu.Lock()
	return func() {
		second.mu.Unlock()
		first.mu.Unlock()
	}
}

// Merge folds other into s; other is unchanged. See SPEC.md §7.
func (s *Sketch) Merge(o sketch.Sketch) error {
	other, ok := o.(*Sketch)
	if !ok {
		return ErrIncompatible
	}
	if s == other {
		return ErrSelfMerge
	}
	unlock := lockPair(s, other)
	defer unlock()
	if s.p != other.p {
		return ErrIncompatible
	}
	// Build the entire result in an isolated copy.  This keeps the receiver
	// unchanged for overflow, invalid aggregate, or identity failures.
	staged := s.cloneLocked()
	if err := staged.mergeLocked(other); err != nil {
		return err
	}
	s.copyFromLocked(staged)
	return nil
}

func (s *Sketch) copyFromLocked(n *Sketch) {
	s.p, s.enc, s.cs = n.p, n.enc, n.cs
	s.floor, s.w, s.wSum, s.wq, s.wqSum = n.floor, n.w, n.wSum, n.wq, n.wqSum
	s.orphaned, s.rejected = n.orphaned, n.rejected
	s.promoted, s.reAnchored, s.dissolved = n.promoted, n.reAnchored, n.dissolved
	s.fused, s.aliased, s.dropped = n.fused, n.aliased, n.dropped
}

func addMerge(a, b uint64) (uint64, error) {
	v, ok := addOK(a, b)
	if !ok {
		return 0, ErrOverflow
	}
	return v, nil
}

func (s *Sketch) mergeLocked(other *Sketch) error {

	ubA, ubB := s.ubar(), other.ubar()
	pre := s.cs
	used := make(map[uint64]struct{}, len(pre)+len(other.cs))
	for _, c := range pre {
		used[c.id.id] = struct{}{}
	}
	fusedA := map[*counter]bool{}
	var added []*counter

	bs := append([]*counter(nil), other.cs...)
	sort.Slice(bs, func(i, j int) bool { return bs[i].id.id < bs[j].id.id })
	for _, bc := range bs {
		hits, confident := s.classify(pre, bc.id.code)
		switch {
		case confident:
			if err := s.fuse(hits[0].c, bc); err != nil {
				return err
			}
			fusedA[hits[0].c] = true
			var err error
			if s.fused, err = addMerge(s.fused, 1); err != nil {
				return err
			}
		case len(hits) > 0:
			nc := bc.clone()
			ensureMergedID(nc, used)
			for _, h := range hits[:min(2, len(hits))] {
				h.c.id.aliases = appendUnique(h.c.id.aliases, nc.id.id)
				nc.id.aliases = appendUnique(nc.id.aliases, h.c.id.id)
			}
			added = append(added, nc)
			var err error
			if s.aliased, err = addMerge(s.aliased, 1); err != nil {
				return err
			}
		default:
			nc := bc.clone()
			ensureMergedID(nc, used)
			added = append(added, nc)
		}
	}

	// One-sided counters absorb the other side's unseen mass (step 4).
	for _, c := range pre {
		if !fusedA[c] {
			var err error
			c.agg.floorAdd, err = addMerge(c.agg.floorAdd, ubB)
			if err != nil {
				return err
			}
		}
	}
	for _, c := range added {
		var err error
		c.agg.floorAdd, err = addMerge(c.agg.floorAdd, ubA)
		if err != nil {
			return err
		}
	}

	all := append(append([]*counter(nil), pre...), added...)
	floor, err := addMerge(ubA, ubB)
	if err != nil {
		return err
	}
	if len(all) > s.p.Capacity {
		sort.SliceStable(all, func(i, j int) bool {
			ui, uj := all[i].agg.upper(), all[j].agg.upper()
			if ui != uj {
				return ui > uj
			}
			return all[i].id.id < all[j].id.id
		})
		for _, c := range all[s.p.Capacity:] {
			floor = max(floor, c.agg.upper())
			if s.orphaned, err = addMerge(s.orphaned, c.unc.count); err != nil {
				return err
			}
			if s.dropped, err = addMerge(s.dropped, 1); err != nil {
				return err
			}
		}
		all = all[:s.p.Capacity]
	}
	s.cs = all
	s.floor = floor

	if s.w, err = addMerge(s.w, other.w); err != nil {
		return err
	}
	s.wSum.addKahan(other.wSum)
	if s.wq, err = addMerge(s.wq, other.wq); err != nil {
		return err
	}
	s.wqSum.addKahan(other.wqSum)
	if s.orphaned, err = addMerge(s.orphaned, other.orphaned); err != nil {
		return err
	}
	if err := s.rejected.add(other.rejected); err != nil {
		return err
	}
	if s.promoted, err = addMerge(s.promoted, other.promoted); err != nil {
		return err
	}
	if s.reAnchored, err = addMerge(s.reAnchored, other.reAnchored); err != nil {
		return err
	}
	if s.dissolved, err = addMerge(s.dissolved, other.dissolved); err != nil {
		return err
	}
	if s.fused, err = addMerge(s.fused, other.fused); err != nil {
		return err
	}
	if s.aliased, err = addMerge(s.aliased, other.aliased); err != nil {
		return err
	}
	if s.dropped, err = addMerge(s.dropped, other.dropped); err != nil {
		return err
	}
	if !finite(s.wSum.sum, s.wSum.c, s.wqSum.sum, s.wqSum.c) {
		return fmt.Errorf("semantic: non-finite merged aggregate")
	}

	// Deferred promotion checks (step 7), in id order.
	var check []*counter
	for _, c := range s.cs {
		if fusedA[c] && c.id.provisional && s.p.Probation > 0 && len(c.id.members) >= s.p.Probation {
			check = append(check, c)
		}
	}
	sort.Slice(check, func(i, j int) bool { return check[i].id.id < check[j].id.id })
	for _, c := range check {
		s.checkProbation(c)
	}
	return nil
}

// ensureMergedID preserves unique canonical IDs even when independent workers
// happened to choose the same numeric identifier for unrelated entries.
func ensureMergedID(c *counter, used map[uint64]struct{}) {
	if _, exists := used[c.id.id]; !exists {
		used[c.id.id] = struct{}{}
		return
	}
	base := c.id.id
	for salt := uint64(1); ; salt++ {
		id := quant.Mix64(base ^ quant.Mix64(salt))
		if _, exists := used[id]; !exists {
			c.id.id = id
			used[id] = struct{}{}
			return
		}
	}
}

// fuse adds src (from the other sketch) into dst, which keeps its code.
func (s *Sketch) fuse(dst, src *counter) error {
	var err error
	if dst.agg.g, err = addMerge(dst.agg.g, src.agg.g); err != nil {
		return err
	}
	dst.agg.G.addKahan(src.agg.G)
	if dst.agg.inh, err = addMerge(dst.agg.inh, src.agg.inh); err != nil {
		return err
	}
	if dst.agg.floorAdd, err = addMerge(dst.agg.floorAdd, src.agg.floorAdd); err != nil {
		return err
	}
	dst.agg.inhSum += src.agg.inhSum
	if math.IsNaN(dst.agg.inhSum) || math.IsInf(dst.agg.inhSum, 0) {
		return fmt.Errorf("semantic: non-finite inherited sum")
	}
	if dst.unc.count, err = addMerge(dst.unc.count, src.unc.count); err != nil {
		return err
	}
	dst.unc.sum.addKahan(src.unc.sum)

	if src.id.id < dst.id.id {
		dst.id.aliases = appendUnique(dst.id.aliases, dst.id.id)
		dst.id.id = src.id.id
	} else {
		dst.id.aliases = appendUnique(dst.id.aliases, src.id.id)
	}
	for _, a := range src.id.aliases {
		dst.id.aliases = appendUnique(dst.id.aliases, a)
	}
	if uint64(dst.id.confident)+uint64(src.id.confident) > math.MaxUint32 {
		return ErrOverflow
	}
	dst.id.confident += src.id.confident

	if dst.id.provisional && src.id.provisional {
		for _, m := range src.id.members {
			s.addMember(dst, m.code.Clone(), m.item)
		}
	} else {
		dst.id.provisional = false
		dst.id.members = nil
	}

	for _, e := range src.ex {
		s.offerExemplar(dst, e.item)
	}
	return nil
}

func appendUnique(xs []uint64, v uint64) []uint64 {
	// Keep ascending IDs for deterministic serialization and bounded merge
	// memory.  On exhaustion we retain the lowest IDs: truncation is explicit
	// historical metadata loss and never changes matching confidence.
	i := sort.Search(len(xs), func(i int) bool { return xs[i] >= v })
	if i < len(xs) && xs[i] == v {
		return xs
	}
	if len(xs) >= maxAliasesPerCounter && (i == len(xs) || v > xs[len(xs)-1]) {
		return xs
	}
	xs = append(xs, 0)
	copy(xs[i+1:], xs[i:])
	xs[i] = v
	if len(xs) > maxAliasesPerCounter {
		xs = xs[:maxAliasesPerCounter]
	}
	return xs
}
