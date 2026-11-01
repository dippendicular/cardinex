package semantic

import (
	"sort"

	"github.com/dippendicular/cardinex/core/sketch"
)

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
	if s.p != other.p {
		return ErrIncompatible
	}
	unlock := lockPair(s, other)
	defer unlock()

	ubA, ubB := s.ubar(), other.ubar()
	pre := s.cs
	fusedA := map[*counter]bool{}
	var added []*counter

	bs := append([]*counter(nil), other.cs...)
	sort.Slice(bs, func(i, j int) bool { return bs[i].id.id < bs[j].id.id })
	for _, bc := range bs {
		hits, confident := s.classify(pre, bc.id.code)
		switch {
		case confident:
			s.fuse(hits[0].c, bc)
			fusedA[hits[0].c] = true
			s.fused++
		case len(hits) > 0:
			nc := bc.clone()
			for _, h := range hits[:min(2, len(hits))] {
				h.c.id.aliases = appendUnique(h.c.id.aliases, nc.id.id)
				nc.id.aliases = appendUnique(nc.id.aliases, h.c.id.id)
			}
			added = append(added, nc)
			s.aliased++
		default:
			added = append(added, bc.clone())
		}
	}

	// One-sided counters absorb the other side's unseen mass (step 4).
	for _, c := range pre {
		if !fusedA[c] {
			c.agg.floorAdd += ubB
		}
	}
	for _, c := range added {
		c.agg.floorAdd += ubA
	}

	all := append(append([]*counter(nil), pre...), added...)
	floor := ubA + ubB
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
			s.orphaned += c.unc.count
			s.dropped++
		}
		all = all[:s.p.Capacity]
	}
	s.cs = all
	s.floor = floor

	s.w += other.w
	s.wSum.addKahan(other.wSum)
	s.wq += other.wq
	s.wqSum.addKahan(other.wqSum)
	s.orphaned += other.orphaned
	s.rejected.add(other.rejected)
	s.promoted += other.promoted
	s.reAnchored += other.reAnchored
	s.dissolved += other.dissolved
	s.fused += other.fused
	s.aliased += other.aliased
	s.dropped += other.dropped

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

// fuse adds src (from the other sketch) into dst, which keeps its code.
func (s *Sketch) fuse(dst, src *counter) {
	dst.agg.g += src.agg.g
	dst.agg.G.addKahan(src.agg.G)
	dst.agg.inh += src.agg.inh
	dst.agg.floorAdd += src.agg.floorAdd
	dst.agg.inhSum += src.agg.inhSum

	dst.unc.count += src.unc.count
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
}

func appendUnique(xs []uint64, v uint64) []uint64 {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
