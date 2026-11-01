package semantic

import (
	"errors"
	"math"
)

// ErrNotSeparated means no tau satisfies the separation condition (V) for the
// given geometry: Δ > 4ρ + 4η + 2·band fails.
var ErrNotSeparated = errors.New("semantic: no tau satisfies Δ > 4ρ + 4η + 2·band")

// TauRange returns the interval [lo, hi) of thresholds that satisfy (V)
// (docs/SPEC.md §2): 2ρ + 2η + band ≤ τ < Δ − 2ρ − 2η − band. All arguments
// are angles in radians; rho, delta and eta are measured on the data.
func TauRange(rho, delta, eta, band float64) (lo, hi float64, err error) {
	if rho < 0 || eta < 0 || band < 0 || delta <= 0 || math.IsNaN(rho+delta+eta+band) {
		return 0, 0, errors.New("semantic: rho, eta, band must be >= 0 and delta > 0")
	}
	lo = 2*rho + 2*eta + band
	hi = math.Min(delta-2*rho-2*eta-band, math.Pi-band)
	if lo <= band {
		lo = math.Nextafter(band, math.Inf(1))
	}
	if lo >= hi {
		return 0, 0, ErrNotSeparated
	}
	return lo, hi, nil
}

// TauFor returns the midpoint of TauRange, which leaves equal slack on the
// same-entity side and the cross-entity side.
func TauFor(rho, delta, eta, band float64) (float64, error) {
	lo, hi, err := TauRange(rho, delta, eta, band)
	if err != nil {
		return 0, err
	}
	return (lo + hi) / 2, nil
}
