// Command eta measures the η event (SPEC §3.3) with the exact seeded
// randomized-Hadamard rotation: the per-comparison error |θ̂ − θ| at
// d ∈ {384, 768, 1024} and bits ∈ {1, 2, 4}, and the probability that it
// exceeds 2η for several η. It also prints the union bound over
// N × capacity comparisons so its looseness at 1 bit is visible.
//
//	go run ./experiments/cmd/eta -out experiments/out/eta.csv
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

	"github.com/dippendicular/cardinex/core/quant"
	"github.com/dippendicular/cardinex/experiments/internal/synth"
)

var (
	pairs    = flag.Int("pairs", 20000, "pairs per (d, bits, angle)")
	n        = flag.Float64("n", 1e8, "stream length for the union bound")
	capacity = flag.Float64("capacity", 4096, "capacity for the union bound")
	out      = flag.String("out", "experiments/out/eta.csv", "output CSV")
)

func main() {
	flag.Parse()
	must(os.MkdirAll(filepath.Dir(*out), 0o755))
	f, err := os.Create(*out)
	must(err)
	defer f.Close()
	cw := csv.NewWriter(f)
	defer cw.Flush()
	etas := []float64{0.01, 0.02, 0.05, 0.1}
	head := []string{"d", "bits", "theta", "mean_abs_err", "p99_abs_err", "max_abs_err"}
	for _, e := range etas {
		head = append(head, fmt.Sprintf("fail_eta_%.2f", e), fmt.Sprintf("union_eta_%.2f", e))
	}
	must(cw.Write(head))

	r := rand.New(rand.NewPCG(1, 2))
	for _, d := range []int{384, 768, 1024} {
		for _, b := range []uint8{1, 2, 4} {
			enc, err := quant.NewEncoder(d, b, 42)
			must(err)
			for _, theta := range []float64{0.05, 0.1, 0.2, 0.4, 0.8, 1.2, 1.57} {
				errs := make([]float64, *pairs)
				for i := range errs {
					x := synth.Unit(r, d)
					y := synth.Rotate(r, x, theta)
					cx, _ := enc.Encode(synth.F32(x))
					cy, _ := enc.Encode(synth.F32(y))
					errs[i] = math.Abs(enc.Distance(cx, cy) - theta)
				}
				sort.Float64s(errs)
				var sum float64
				for _, e := range errs {
					sum += e
				}
				row := []string{fmt.Sprint(d), fmt.Sprint(b), fmt.Sprintf("%.2f", theta),
					fmt.Sprintf("%.5f", sum/float64(len(errs))), fmt.Sprintf("%.5f", errs[len(errs)*99/100]), fmt.Sprintf("%.5f", errs[len(errs)-1])}
				for _, eta := range etas {
					k := sort.SearchFloat64s(errs, 2*eta)
					p := float64(len(errs)-k) / float64(len(errs))
					row = append(row, fmt.Sprintf("%.6f", p), fmt.Sprintf("%.3g", math.Min(1, p**n**capacity)))
				}
				must(cw.Write(row))
			}
			log.Printf("d=%d bits=%d done", d, b)
		}
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
