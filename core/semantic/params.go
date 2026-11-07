package semantic

import (
	"errors"
	"fmt"
	"math"
)

// Params configures a semantic sketch. See docs/SPEC.md §2.
type Params struct {
	Dim       int     `json:"dim"`
	Capacity  int     `json:"capacity"`
	Tau       float64 `json:"tau"`  // assignment threshold, radians
	Band      float64 `json:"band"` // half-width of the ambiguity band, radians
	Bits      uint8   `json:"bits"`
	ValueMin  float64 `json:"value_min"`
	ValueMax  float64 `json:"value_max"`
	Exemplars int     `json:"exemplars"`
	Probation int     `json:"probation"` // member codes before the promotion check; 0 disables
	Refine    bool    `json:"refine"`
	Seed      uint64  `json:"seed"`
}

// DefaultParams fills the SPEC.md §2 defaults around the required fields.
func DefaultParams(dim int, tau float64, seed uint64) Params {
	return Params{
		Dim: dim, Capacity: 4096, Tau: tau, Band: 0.05, Bits: 1,
		ValueMin: 0, ValueMax: 1, Exemplars: 8, Probation: 3, Seed: seed,
	}
}

var ErrRefineUnsupported = errors.New("semantic: refine is not implemented in model v0")

func (p Params) validate() error {
	finite := func(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
	switch {
	case p.Dim < 1:
		return errors.New("semantic: dim must be positive")
	case p.Capacity < 1:
		return errors.New("semantic: capacity must be positive")
	case !finite(p.Tau) || !finite(p.Band) || p.Band < 0 || p.Tau-p.Band <= 0 || p.Tau+p.Band >= math.Pi:
		return fmt.Errorf("semantic: need 0 < tau-band and tau+band < pi (tau=%v band=%v)", p.Tau, p.Band)
	case p.Bits != 1 && p.Bits != 2 && p.Bits != 4:
		return errors.New("semantic: bits must be 1, 2 or 4")
	case !finite(p.ValueMin) || !finite(p.ValueMax) || p.ValueMin < 0 || p.ValueMax < p.ValueMin:
		return errors.New("semantic: need 0 <= value_min <= value_max, both finite")
	case p.Exemplars < 0 || p.Exemplars > 1024:
		return errors.New("semantic: exemplars must be in [0, 1024]")
	case p.Probation < 0 || p.Probation > 64:
		return errors.New("semantic: probation must be in [0, 64]")
	case p.Refine:
		return ErrRefineUnsupported
	}
	// Keep construction and decoding within the practical limits enforced by
	// the binary decoder.  This also bounds the encoder's sign matrices.
	if p.Dim > maxDim || p.Capacity > maxCapacity {
		return errors.New("semantic: dim or capacity exceeds implementation limit")
	}
	return nil
}
