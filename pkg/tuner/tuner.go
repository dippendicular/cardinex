package tuner

import (
	"errors"
	"math"
)

type Params struct {
	Type             string  `json:"type"`
	MemoryBytes      int     `json:"memory_bytes"`
	ExpectedElements int     `json:"expected_elements"`
	ErrorRate        float64 `json:"error_rate"`
	Confidence       float64 `json:"confidence"`
	Precision        uint8   `json:"precision,omitempty"`
	Width            uint32  `json:"width,omitempty"`
	Depth            uint32  `json:"depth,omitempty"`
	Bits             uint64  `json:"bits,omitempty"`
	Hashes           uint32  `json:"hashes,omitempty"`
}

type Result struct {
	HyperLogLog Params   `json:"hyperloglog"`
	CountMin    Params   `json:"countmin"`
	Bloom       Params   `json:"bloom"`
	Comparison  []Params `json:"comparison"`
}

func Tune(maxMemoryBytes int, targetErrorRate float64, expectedElements int) (Result, error) {
	if maxMemoryBytes <= 0 || targetErrorRate <= 0 || targetErrorRate >= 1 || expectedElements <= 0 {
		return Result{}, errors.New("invalid tuning inputs")
	}
	h := tuneHLL(maxMemoryBytes)
	c := tuneCMS(maxMemoryBytes, targetErrorRate)
	b := tuneBloom(maxMemoryBytes, targetErrorRate, expectedElements)
	return Result{HyperLogLog: h, CountMin: c, Bloom: b, Comparison: []Params{h, c, b}}, nil
}

func tuneHLL(maxMemoryBytes int) Params {
	p := uint8(4)
	for p < 18 && (1<<uint(p+1)) <= maxMemoryBytes {
		p++
	}
	m := 1 << p
	return Params{Type: "hll", MemoryBytes: m, ErrorRate: 1.04 / math.Sqrt(float64(m)), Confidence: 0.95, Precision: p}
}

func tuneCMS(maxMemoryBytes int, targetErrorRate float64) Params {
	depth := uint32(math.Ceil(math.Log(1/(1-0.99)) / math.Log(math.E)))
	width := uint32(maxMemoryBytes / int(depth) / 8)
	minWidth := uint32(math.Ceil(math.E / targetErrorRate))
	if width < minWidth {
		width = minWidth
		depth = uint32(max(1, maxMemoryBytes/(int(width)*8)))
	}
	mem := int(width*depth) * 8
	return Params{Type: "cms", MemoryBytes: mem, ErrorRate: math.E / float64(width), Confidence: 1 - math.Pow(1/math.E, float64(depth)), Width: width, Depth: depth}
}

func tuneBloom(maxMemoryBytes int, targetErrorRate float64, expectedElements int) Params {
	maxBits := uint64(maxMemoryBytes * 8)
	needBits := uint64(math.Ceil(-float64(expectedElements) * math.Log(targetErrorRate) / (math.Ln2 * math.Ln2)))
	bits := needBits
	if bits > maxBits {
		bits = maxBits
	}
	k := uint32(math.Ceil((float64(bits) / float64(expectedElements)) * math.Ln2))
	err := math.Pow(1-math.Exp(-float64(k)*float64(expectedElements)/float64(bits)), float64(k))
	return Params{Type: "bloom", MemoryBytes: int((bits + 7) / 8), ExpectedElements: expectedElements, ErrorRate: err, Confidence: 1 - err, Bits: bits, Hashes: k}
}
