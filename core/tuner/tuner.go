// Package tuner plans sketch parameters from error targets and memory budgets.
// Every plan either meets all of its constraints or returns an
// *InfeasibleError naming the constraint that cannot be met together with
// the closest feasible plan.
package tuner

import (
	"fmt"
	"math"

	"github.com/dippendicular/cardinex/core/classic"
)

// InfeasibleError reports a constraint that cannot be met.
type InfeasibleError struct {
	Constraint string // "memory" or "error"
	Closest    any    // the plan that best meets the other constraint
}

func (e *InfeasibleError) Error() string {
	return fmt.Sprintf("tuner: %s constraint cannot be met; closest plan %+v", e.Constraint, e.Closest)
}

// HLLPlan is a HyperLogLog configuration.
type HLLPlan struct {
	Precision     uint8
	MemoryBytes   int
	RelativeError float64 // 1.04/√m
}

func hllPlan(p uint8) HLLPlan {
	m := 1 << p
	return HLLPlan{Precision: p, MemoryBytes: (m*6+7)/8 + 1, RelativeError: 1.04 / math.Sqrt(float64(m))}
}

// HLL picks the smallest precision whose relative error is ≤ relErr and
// whose memory is ≤ maxBytes (0 = unlimited).
func HLL(relErr float64, maxBytes int) (HLLPlan, error) {
	for p := uint8(4); p <= 18; p++ {
		pl := hllPlan(p)
		if maxBytes > 0 && pl.MemoryBytes > maxBytes {
			if p == 4 {
				return HLLPlan{}, &InfeasibleError{"memory", pl}
			}
			return HLLPlan{}, &InfeasibleError{"error", hllPlan(p - 1)}
		}
		if pl.RelativeError <= relErr {
			return pl, nil
		}
	}
	return HLLPlan{}, &InfeasibleError{"error", hllPlan(18)}
}

// CMSPlan is a Count-Min configuration.
type CMSPlan struct {
	Width, Depth uint32
	MemoryBytes  int
	Epsilon      float64 // e/width
	Confidence   float64 // 1 − e^−depth
}

func cmsPlan(w, d uint32) CMSPlan {
	return CMSPlan{Width: w, Depth: d, MemoryBytes: int(uint64(w) * uint64(d) * 8),
		Epsilon: math.E / float64(w), Confidence: 1 - math.Exp(-float64(d))}
}

// CMS sizes for additive error eps·N with probability 1 − delta within maxBytes (0 = unlimited).
func CMS(eps, delta float64, maxBytes int) (CMSPlan, error) {
	if !(eps > 0 && eps < 1) || !(delta > 0 && delta < 1) {
		return CMSPlan{}, fmt.Errorf("tuner: need 0 < eps, delta < 1")
	}
	w := uint32(math.Ceil(math.E / eps))
	d := uint32(math.Ceil(math.Log(1 / delta)))
	pl := cmsPlan(w, d)
	if uint64(w)*uint64(d) > classic.MaxCMSCounters {
		return CMSPlan{}, &InfeasibleError{"error", pl}
	}
	if maxBytes > 0 && pl.MemoryBytes > maxBytes {
		// Keep depth (confidence), shrink width to the budget.
		cw := uint32(maxBytes / 8 / int(d))
		if cw == 0 {
			return CMSPlan{}, &InfeasibleError{"memory", cmsPlan(1, 1)}
		}
		return CMSPlan{}, &InfeasibleError{"error", cmsPlan(cw, d)}
	}
	return pl, nil
}

// BloomPlan is a Bloom filter configuration.
type BloomPlan struct {
	Bits        uint64
	Hashes      uint32
	MemoryBytes int
	FPR         float64 // expected at n inserts
}

func bloomPlan(m uint64, k uint32, n int) BloomPlan {
	fpr := math.Pow(1-math.Exp(-float64(k)*float64(n)/float64(m)), float64(k))
	return BloomPlan{Bits: m, Hashes: k, MemoryBytes: int((m + 63) / 64 * 8), FPR: fpr}
}

// Bloom sizes for n keys at false-positive rate fpr within maxBytes (0 = unlimited).
func Bloom(n int, fpr float64, maxBytes int) (BloomPlan, error) {
	if n <= 0 || !(fpr > 0 && fpr < 1) {
		return BloomPlan{}, fmt.Errorf("tuner: need n > 0 and 0 < fpr < 1")
	}
	m := uint64(math.Ceil(-float64(n) * math.Log(fpr) / (math.Ln2 * math.Ln2)))
	k := uint32(math.Max(1, math.Min(64, math.Round(float64(m)/float64(n)*math.Ln2))))
	if m > classic.MaxBloomBits {
		return BloomPlan{}, &InfeasibleError{"error", bloomPlan(classic.MaxBloomBits, k, n)}
	}
	if maxBytes > 0 && int((m+63)/64*8) > maxBytes {
		bm := uint64(maxBytes) * 8
		bk := uint32(math.Max(1, math.Min(64, math.Round(float64(bm)/float64(n)*math.Ln2))))
		return BloomPlan{}, &InfeasibleError{"error", bloomPlan(bm, bk, n)}
	}
	return bloomPlan(m, k, n), nil
}
