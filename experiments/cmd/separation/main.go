// Command separation runs the Phase 0 separation study on constructed-identity
// groups. Input: embeddings as a float32 .npy (n×d) and group labels as an
// int64 .npy (n). Output (CSV, one file per measure):
//
//	<prefix>_hist.csv     within- and between-group angle histograms (raw and quantized)
//	<prefix>_roc.csv      recall vs false-match rate of one global threshold, raw and bits 1/2/4
//	<prefix>_summary.csv  ρ quantiles, min centroid separation Δ, violation fraction at ρ
//
//	go run ./experiments/cmd/separation -emb x.npy -labels y.npy -prefix experiments/out/disc21
//
// Between-group pairs are sampled (-between) because all pairs are quadratic.
package main

import (
	"encoding/binary"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/dippendicular/cardinex/core/quant"
	"github.com/dippendicular/cardinex/experiments/internal/synth"
)

var (
	embPath = flag.String("emb", "", "float32 .npy, shape (n, d)")
	labPath = flag.String("labels", "", "int64 .npy, shape (n,)")
	prefix  = flag.String("prefix", "experiments/out/separation", "output prefix")
	between = flag.Int("between", 200000, "sampled between-group pairs")
	rhoQ    = flag.Float64("rho-quantile", 0.99, "quantile of item-to-centroid angle used as ρ")
)

func readNpy(path string) (shape []int, data []byte, descr string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	if len(b) < 10 || string(b[1:6]) != "NUMPY" {
		return nil, nil, "", errors.New("not a .npy file")
	}
	hl, off := int(binary.LittleEndian.Uint16(b[8:10])), 10
	if b[6] >= 2 {
		hl, off = int(binary.LittleEndian.Uint32(b[8:12])), 12
	}
	header := string(b[off : off+hl])
	if regexp.MustCompile(`'fortran_order':\s*True`).MatchString(header) {
		return nil, nil, "", errors.New("fortran order not supported")
	}
	descr = regexp.MustCompile(`'descr':\s*'([^']+)'`).FindStringSubmatch(header)[1]
	for _, s := range regexp.MustCompile(`\d+`).FindAllString(regexp.MustCompile(`'shape':\s*\(([^)]*)\)`).FindStringSubmatch(header)[1], -1) {
		v, _ := strconv.Atoi(s)
		shape = append(shape, v)
	}
	return shape, b[off+hl:], descr, nil
}

func main() {
	flag.Parse()
	shape, raw, descr, err := readNpy(*embPath)
	must(err)
	if descr != "<f4" || len(shape) != 2 {
		log.Fatal("embeddings must be little-endian float32 (n, d)")
	}
	n, d := shape[0], shape[1]
	lshape, lraw, ldescr, err := readNpy(*labPath)
	must(err)
	if ldescr != "<i8" || lshape[0] != n {
		log.Fatal("labels must be int64 (n,)")
	}
	vecs := make([][]float64, n)
	labels := make([]int64, n)
	groups := map[int64][]int{}
	for i := 0; i < n; i++ {
		v := make([]float64, d)
		for t := 0; t < d; t++ {
			v[t] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[4*(i*d+t):])))
		}
		vecs[i] = synth.Normalize(v)
		labels[i] = int64(binary.LittleEndian.Uint64(lraw[8*i:]))
		groups[labels[i]] = append(groups[labels[i]], i)
	}

	// ρ: item-to-centroid angles; Δ: min centroid separation; violations at ρ.
	cent := map[int64][]float64{}
	var toCent []float64
	for g, idx := range groups {
		c := make([]float64, d)
		for _, i := range idx {
			for t := range c {
				c[t] += vecs[i][t]
			}
		}
		cent[g] = synth.Normalize(c)
		for _, i := range idx {
			toCent = append(toCent, synth.Angle(vecs[i], cent[g]))
		}
	}
	sort.Float64s(toCent)
	rho := toCent[int(float64(len(toCent)-1)**rhoQ)]
	keys := make([]int64, 0, len(cent))
	for g := range cent {
		keys = append(keys, g)
	}
	minSep := math.Pi
	for a := 0; a < len(keys); a++ {
		for b := a + 1; b < len(keys); b++ {
			minSep = math.Min(minSep, synth.Angle(cent[keys[a]], cent[keys[b]]))
		}
	}
	viol := 0
	for _, x := range toCent {
		if x > rho {
			viol++
		}
	}

	// Pairs: all within-group, sampled between-group.
	type pair struct{ i, j int }
	var within, betw []pair
	for _, idx := range groups {
		for a := 0; a < len(idx); a++ {
			for b := a + 1; b < len(idx); b++ {
				within = append(within, pair{idx[a], idx[b]})
			}
		}
	}
	r := rand.New(rand.NewPCG(1, 1))
	for len(betw) < *between {
		i, j := r.IntN(n), r.IntN(n)
		if labels[i] != labels[j] {
			betw = append(betw, pair{i, j})
		}
	}

	encs := map[string]*quant.Encoder{}
	codes := map[string][]quant.Code{}
	for _, b := range []uint8{1, 2, 4} {
		e, err := quant.NewEncoder(d, b, 42)
		must(err)
		name := fmt.Sprintf("bits%d", b)
		encs[name] = e
		cs := make([]quant.Code, n)
		for i := range vecs {
			cs[i], _ = e.Encode(synth.F32(vecs[i]))
		}
		codes[name] = cs
	}
	dist := func(method string, p pair) float64 {
		if method == "raw" {
			return synth.Angle(vecs[p.i], vecs[p.j])
		}
		return encs[method].Distance(codes[method][p.i], codes[method][p.j])
	}

	must(os.MkdirAll(filepath.Dir(*prefix), 0o755))
	hist := newCSV(*prefix+"_hist.csv", "method", "kind", "bin_lo", "count")
	roc := newCSV(*prefix+"_roc.csv", "method", "threshold", "recall", "false_match_rate")
	for _, m := range []string{"raw", "bits1", "bits2", "bits4"} {
		ws, bs := make([]float64, len(within)), make([]float64, len(betw))
		for i, p := range within {
			ws[i] = dist(m, p)
		}
		for i, p := range betw {
			bs[i] = dist(m, p)
		}
		sort.Float64s(ws)
		sort.Float64s(bs)
		for _, kv := range []struct {
			k  string
			xs []float64
		}{{"within", ws}, {"between", bs}} {
			counts := make([]int, 64)
			for _, x := range kv.xs {
				counts[min(63, int(x/math.Pi*64))]++
			}
			for i, c := range counts {
				hist.row(m, kv.k, fmt.Sprintf("%.4f", float64(i)*math.Pi/64), strconv.Itoa(c))
			}
		}
		for t := 0.0; t <= math.Pi/2; t += 0.01 {
			rec := float64(sort.SearchFloat64s(ws, t+1e-12)) / float64(max(1, len(ws)))
			fmr := float64(sort.SearchFloat64s(bs, t+1e-12)) / float64(max(1, len(bs)))
			roc.row(m, fmt.Sprintf("%.2f", t), fmt.Sprintf("%.5f", rec), fmt.Sprintf("%.6f", fmr))
		}
	}
	hist.close()
	roc.close()
	sum := newCSV(*prefix+"_summary.csv", "items", "groups", "rho_quantile", "rho", "min_centroid_separation", "violation_fraction", "delta_over_rho")
	sum.row(strconv.Itoa(n), strconv.Itoa(len(groups)), fmt.Sprint(*rhoQ), fmt.Sprintf("%.4f", rho),
		fmt.Sprintf("%.4f", minSep), fmt.Sprintf("%.5f", float64(viol)/float64(len(toCent))), fmt.Sprintf("%.3f", minSep/rho))
	sum.close()
}

type csvFile struct {
	f *os.File
	w *csv.Writer
}

func newCSV(path string, head ...string) *csvFile {
	f, err := os.Create(path)
	must(err)
	c := &csvFile{f, csv.NewWriter(f)}
	c.row(head...)
	return c
}

func (c *csvFile) row(xs ...string) { must(c.w.Write(xs)) }
func (c *csvFile) close()           { c.w.Flush(); c.f.Close() }

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
