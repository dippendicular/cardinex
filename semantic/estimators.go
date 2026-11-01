package semantic

// All interval formulas live here and must match docs/ESTIMATORS.md. Every
// function is pure. ieff is I_eff = I + f (ESTIMATORS.md notation).

// Estimate is a point value with a guaranteed error interval [Lower, Upper],
// guaranteed conditional on the η event (docs/ESTIMATORS.md §1). Empty is set
// when there is no genuine mass behind the value.
type Estimate struct {
	Value, Lower, Upper float64
	Empty               bool
}

// CountInterval is ESTIMATORS.md §3: [g, g + I_eff], value g + I_eff.
func CountInterval(g, ieff uint64) Estimate {
	up := float64(g + ieff)
	return Estimate{Value: up, Lower: float64(g), Upper: up}
}

// SumInterval is ESTIMATORS.md §3: [G, G + b·I_eff], value G + inhSum
// clamped into the interval.
func SumInterval(G, inhSum float64, ieff uint64, b float64) Estimate {
	lo := G
	up := G + float64(b*float64(ieff))
	return Estimate{Value: clamp(G+inhSum, lo, up), Lower: lo, Upper: up}
}

// MeanInterval is ESTIMATORS.md §5:
// [(G + a·I_eff)/(g + I_eff), (G + b·I_eff)/(g + I_eff)], value G/g, all
// clamped to [a, b]. With g = 0 it is [a, b] with Empty set.
func MeanInterval(g uint64, G float64, ieff uint64, a, b float64) Estimate {
	if g == 0 {
		return Estimate{Value: float64(0.5 * (a + b)), Lower: a, Upper: b, Empty: true}
	}
	n := float64(g + ieff)
	i := float64(ieff)
	lo := clamp((G+float64(a*i))/n, a, b)
	up := clamp((G+float64(b*i))/n, a, b)
	return Estimate{Value: clamp(G/float64(g), lo, up), Lower: lo, Upper: up}
}

// UnmonitoredCount is the interval for an entity with no counter: [0, Ū].
func UnmonitoredCount(ubar uint64) Estimate {
	return Estimate{Value: 0, Lower: 0, Upper: float64(ubar)}
}

// UnmonitoredSum is the sum interval for an entity with no counter: [0, b·Ū].
func UnmonitoredSum(ubar uint64, b float64) Estimate {
	return Estimate{Value: 0, Lower: 0, Upper: float64(b * float64(ubar))}
}

// UnmonitoredMean is [a, b] with Empty set.
func UnmonitoredMean(a, b float64) Estimate { return MeanInterval(0, 0, 0, a, b) }

// DiffInterval is ESTIMATORS.md §6: x − y as [x.Lower − y.Upper, x.Upper − y.Lower].
func DiffInterval(x, y Estimate) Estimate {
	return Estimate{Value: x.Value - y.Value, Lower: x.Lower - y.Upper, Upper: x.Upper - y.Lower, Empty: x.Empty || y.Empty}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
