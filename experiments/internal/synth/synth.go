// Package synth generates synthetic entity worlds on the unit sphere.
package synth

import (
	"math"
	"math/rand/v2"
)

// Unit returns a random unit vector.
func Unit(r *rand.Rand, d int) []float64 {
	v := make([]float64, d)
	for i := range v {
		v[i] = r.NormFloat64()
	}
	return Normalize(v)
}

// Normalize scales v to unit length in place and returns it.
func Normalize(v []float64) []float64 {
	var n float64
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= n
	}
	return v
}

// Rotate returns cos(phi)·c + sin(phi)·u for a random unit u ⟂ c: a point at
// exactly angle phi from c.
func Rotate(r *rand.Rand, c []float64, phi float64) []float64 {
	u := Unit(r, len(c))
	dot := Dot(u, c)
	for i := range u {
		u[i] -= dot * c[i]
	}
	Normalize(u)
	out := make([]float64, len(c))
	for i := range c {
		out[i] = math.Cos(phi)*c[i] + math.Sin(phi)*u[i]
	}
	return out
}

// Toward returns the point at angle phi from a on the great circle to b.
func Toward(a, b []float64, phi float64) []float64 {
	dot := Dot(a, b)
	u := make([]float64, len(a))
	for i := range a {
		u[i] = b[i] - dot*a[i]
	}
	Normalize(u)
	out := make([]float64, len(a))
	for i := range a {
		out[i] = math.Cos(phi)*a[i] + math.Sin(phi)*u[i]
	}
	return out
}

func Dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// Angle is the angular distance between unit vectors.
func Angle(a, b []float64) float64 { return math.Acos(math.Max(-1, math.Min(1, Dot(a, b)))) }

// F32 converts to float32.
func F32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x)
	}
	return out
}

// Item is one observation.
type Item struct {
	Vec    []float32
	Value  float64
	Entity int // -1 for a violation
	ID     uint64
}

// World is a set of entities and a valid stream over them.
type World struct {
	Centers [][]float64
	Means   []float64
	Items   []Item
}

// NewWorld draws entities random centers and n Zipf(1.1)-distributed valid
// observations within rho of their center.
func NewWorld(r *rand.Rand, d, entities, n int, rho float64) *World {
	w := &World{}
	cum := make([]float64, entities)
	var total float64
	for j := 0; j < entities; j++ {
		w.Centers = append(w.Centers, Unit(r, d))
		w.Means = append(w.Means, r.Float64())
		total += 1 / math.Pow(float64(j+1), 1.1)
		cum[j] = total
	}
	for i := 0; i < n; i++ {
		x := r.Float64() * total
		j := 0
		for cum[j] < x {
			j++
		}
		w.Items = append(w.Items, w.Observe(r, j, rho, uint64(i+1)))
	}
	return w
}

// Observe draws one valid observation of entity j.
func (w *World) Observe(r *rand.Rand, j int, rho float64, id uint64) Item {
	v := math.Max(0, math.Min(1, w.Means[j]+(r.Float64()-0.5)*0.4))
	return Item{Vec: F32(Rotate(r, w.Centers[j], r.Float64()*rho)), Value: v, Entity: j, ID: id}
}

// MinSeparation is the smallest angle between two centers.
func (w *World) MinSeparation() float64 {
	m := math.Pi
	for i := range w.Centers {
		for j := i + 1; j < len(w.Centers); j++ {
			m = math.Min(m, Angle(w.Centers[i], w.Centers[j]))
		}
	}
	return m
}
