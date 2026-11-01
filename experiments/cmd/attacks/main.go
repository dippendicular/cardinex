// Command attacks runs the Phase 0 adversarial harness (IMPLEMENTATION.md §10):
// six attacks, each against a counterfactual control with the same valid
// stream, order, capacity and merge tree, over three merge-tree shapes and
// several stream lengths. It writes one CSV row per run.
//
//	go run ./experiments/cmd/attacks -out experiments/out/attacks.csv
//
// Metrics:
//
//	V          injected (violating) item count
//	E_prop     Σ_j |count_attack(j) − count_control(j)| over true entities,
//	           where count(j) is Count.Value of the counter Lookup(center_j) finds (0 if none)
//	A          (V + E_prop) / V
//	false_merges  counters whose exemplars come from ≥ 2 true entities
//	false_splits  extra counters for entities that already have one
//	hh_recall     fraction of the true top-10 entities found by Lookup
//	bytes         serialized size of the attacked sketch
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/dippendicular/cardinex/core/semantic"
	"github.com/dippendicular/cardinex/experiments/internal/synth"
)

var (
	dim      = flag.Int("dim", 256, "embedding dimension")
	entities = flag.Int("entities", 80, "true entities")
	capacity = flag.Int("capacity", 32, "sketch capacity")
	rho      = flag.Float64("rho", 0.05, "entity radius (radians)")
	tau      = flag.Float64("tau", 0.35, "assignment threshold")
	band     = flag.Float64("band", 0.05, "ambiguity band")
	shards   = flag.Int("shards", 8, "shards per merge tree")
	seed     = flag.Uint64("seed", 1, "seed")
	out      = flag.String("out", "experiments/out/attacks.csv", "output CSV")
)

type attack struct {
	name string
	// inject returns violating items and the shard each goes to, and may
	// rewrite the world (attack 4 places two entities deliberately).
	inject func(r *rand.Rand, w *synth.World) (items []synth.Item, shard []int)
}

func midpoint(a, b []float64) []float64 {
	m := make([]float64, len(a))
	for i := range a {
		m[i] = a[i] + b[i]
	}
	return synth.Normalize(m)
}

func heaviest(w *synth.World, k int) []int {
	n := make([]int, len(w.Centers))
	for _, it := range w.Items {
		n[it.Entity]++
	}
	idx := make([]int, len(n))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return n[idx[i]] > n[idx[j]] })
	return idx[:k]
}

func violation(v []float64, id uint64) synth.Item {
	return synth.Item{Vec: synth.F32(v), Value: 1, Entity: -1, ID: id}
}

var attacks = []attack{
	{"boundary_seed", func(r *rand.Rand, w *synth.World) ([]synth.Item, []int) {
		h := heaviest(w, 2)
		return []synth.Item{violation(midpoint(w.Centers[h[0]], w.Centers[h[1]]), 1<<40)}, []int{0}
	}},
	{"drift_injection", func(r *rand.Rand, w *synth.World) ([]synth.Item, []int) {
		// A burst aimed at the band edge of a light (young) entity, toward a heavy one.
		h := heaviest(w, len(w.Centers))
		young, heavy := h[len(h)-1], h[0]
		var items []synth.Item
		var sh []int
		for i := 0; i < 20; i++ {
			items = append(items, violation(synth.Toward(w.Centers[young], w.Centers[heavy], *tau+(r.Float64()-0.5)*2**band), uint64(1<<40+i)))
			sh = append(sh, 0)
		}
		return items, sh
	}},
	{"merge_fusion", func(r *rand.Rand, w *synth.World) ([]synth.Item, []int) {
		// Shard 1 gets a fake entity whose representative sits in the band of a real one.
		h := heaviest(w, 1)
		fake := synth.Toward(w.Centers[h[0]], synth.Unit(r, len(w.Centers[0])), *tau)
		var items []synth.Item
		var sh []int
		for i := 0; i < 10; i++ {
			items = append(items, violation(synth.Rotate(r, fake, r.Float64()**rho), uint64(1<<40+i)))
			sh = append(sh, 1%*shards)
		}
		return items, sh
	}},
	{"outlier_seed", func(r *rand.Rand, w *synth.World) ([]synth.Item, []int) {
		// Place entities 0 and 1 at Δ = 5ρ + band apart (< 6ρ), seed their midpoint first.
		delta := 5**rho + *band
		w.Centers[1] = synth.Toward(w.Centers[0], w.Centers[1], delta)
		for i := range w.Items {
			if e := w.Items[i].Entity; e == 0 || e == 1 {
				w.Items[i] = w.Observe(r, e, *rho, w.Items[i].ID)
			}
		}
		return []synth.Item{violation(midpoint(w.Centers[0], w.Centers[1]), 1<<40)}, []int{0}
	}},
	{"hidden_second_entity", func(r *rand.Rand, w *synth.World) ([]synth.Item, []int) {
		// Ambiguous point between the heaviest entity and the lightest one (likely unseen early).
		h := heaviest(w, len(w.Centers))
		return []synth.Item{violation(synth.Toward(w.Centers[h[0]], w.Centers[h[len(h)-1]], *tau-*band/2), 1<<40)}, []int{0}
	}},
	{"probation_poisoning", func(r *rand.Rand, w *synth.World) ([]synth.Item, []int) {
		// Consistent outliers around a false center between two heavy entities.
		h := heaviest(w, 2)
		f := midpoint(w.Centers[h[0]], w.Centers[h[1]])
		var items []synth.Item
		var sh []int
		for i := 0; i < 6; i++ {
			items = append(items, violation(synth.Rotate(r, f, r.Float64()**rho), uint64(1<<40+i)))
			sh = append(sh, 0)
		}
		return items, sh
	}},
}

type tree func(sk []*semantic.Sketch, r *rand.Rand) *semantic.Sketch

var trees = map[string]tree{
	"sequential": func(sk []*semantic.Sketch, _ *rand.Rand) *semantic.Sketch {
		for _, x := range sk[1:] {
			must(sk[0].Merge(x))
		}
		return sk[0]
	},
	"balanced": func(sk []*semantic.Sketch, _ *rand.Rand) *semantic.Sketch {
		for len(sk) > 1 {
			var next []*semantic.Sketch
			for i := 0; i+1 < len(sk); i += 2 {
				must(sk[i].Merge(sk[i+1]))
				next = append(next, sk[i])
			}
			if len(sk)%2 == 1 {
				next = append(next, sk[len(sk)-1])
			}
			sk = next
		}
		return sk[0]
	},
	"random": func(sk []*semantic.Sketch, r *rand.Rand) *semantic.Sketch {
		for len(sk) > 1 {
			i, j := r.IntN(len(sk)), r.IntN(len(sk)-1)
			if j >= i {
				j++
			}
			must(sk[i].Merge(sk[j]))
			sk = append(sk[:j], sk[j+1:]...)
		}
		return sk[0]
	},
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// run builds shards (stream split round-robin, violations inserted halfway
// through their shard's stream) and merges them with t.
func run(p semantic.Params, w *synth.World, viol []synth.Item, vsh []int, t tree, treeSeed uint64) *semantic.Sketch {
	sk := make([]*semantic.Sketch, *shards)
	per := make([][]synth.Item, *shards)
	for i, it := range w.Items {
		per[i%*shards] = append(per[i%*shards], it)
	}
	for i, it := range viol {
		s := vsh[i]
		mid := len(per[s]) / 2
		per[s] = append(per[s][:mid], append([]synth.Item{it}, per[s][mid:]...)...)
	}
	for s := range sk {
		x, err := semantic.New(p)
		must(err)
		for _, it := range per[s] {
			_ = x.Add(it.Vec, it.Value, it.ID)
		}
		sk[s] = x
	}
	return t(sk, rand.New(rand.NewPCG(treeSeed, 7)))
}

type metrics struct {
	counts       []float64
	falseMerges  int
	falseSplits  int
	hhRecall     float64
	bytes        int
}

func measure(s *semantic.Sketch, w *synth.World, owner map[uint64]int) metrics {
	m := metrics{counts: make([]float64, len(w.Centers))}
	for j, c := range w.Centers {
		if e, ok := s.Lookup(synth.F32(c)); ok {
			m.counts[j] = e.Count.Value
		}
	}
	seen := map[int]bool{}
	for _, e := range s.TopK(0, semantic.ByCount) {
		ents := map[int]bool{}
		for _, x := range e.Exemplars {
			if o, ok := owner[x]; ok {
				ents[o] = true
			}
		}
		if len(ents) >= 2 {
			m.falseMerges++
		}
		for o := range ents {
			if seen[o] {
				m.falseSplits++
			}
			seen[o] = true
		}
	}
	top := heaviest(w, min(10, len(w.Centers)))
	hit := 0
	for _, j := range top {
		if m.counts[j] > 0 {
			hit++
		}
	}
	m.hhRecall = float64(hit) / float64(len(top))
	b, _ := s.MarshalBinary()
	m.bytes = len(b)
	return m
}

func main() {
	flag.Parse()
	must(os.MkdirAll(filepath.Dir(*out), 0o755))
	f, err := os.Create(*out)
	must(err)
	defer f.Close()
	cw := csv.NewWriter(f)
	defer cw.Flush()
	must(cw.Write([]string{"attack", "tree", "n", "probation", "V", "E_prop", "A", "false_merges", "false_splits", "hh_recall", "bytes"}))

	for _, n := range []int{2000, 8000, 32000} {
		for _, a := range attacks {
			for _, prob := range []int{0, 3} {
				for _, tname := range []string{"sequential", "balanced", "random"} {
					r := rand.New(rand.NewPCG(*seed, uint64(n)))
					w := synth.NewWorld(r, *dim, *entities, n, *rho)
					viol, vsh := a.inject(r, w)
					owner := map[uint64]int{}
					for _, it := range w.Items {
						owner[it.ID] = it.Entity
					}
					p := semantic.DefaultParams(*dim, *tau, *seed)
					p.Capacity, p.Band, p.Probation = *capacity, *band, prob
					ctl := measure(run(p, w, nil, nil, trees[tname], *seed), w, owner)
					att := measure(run(p, w, viol, vsh, trees[tname], *seed), w, owner)
					var eprop float64
					for j := range ctl.counts {
						eprop += math.Abs(att.counts[j] - ctl.counts[j])
					}
					v := float64(len(viol))
					must(cw.Write([]string{a.name, tname, strconv.Itoa(n), strconv.Itoa(prob), fmt.Sprint(len(viol)),
						fmt.Sprintf("%.0f", eprop), fmt.Sprintf("%.3f", (v+eprop)/v),
						strconv.Itoa(att.falseMerges), strconv.Itoa(att.falseSplits),
						fmt.Sprintf("%.2f", att.hhRecall), strconv.Itoa(att.bytes)}))
				}
			}
		}
	}
	log.Printf("wrote %s", *out)
}
