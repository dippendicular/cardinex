package pkg

import (
	"github.com/dippendicular/cardinex/pkg/bloom"
	"github.com/dippendicular/cardinex/pkg/cms"
	"github.com/dippendicular/cardinex/pkg/hll"
	"github.com/dippendicular/cardinex/pkg/sketch"
)

type Sketch = sketch.Sketch
type HyperLogLog = hll.HyperLogLog
type CountMinSketch = cms.CountMinSketch
type BloomFilter = bloom.BloomFilter

func NewHyperLogLog(precision uint8) (*HyperLogLog, error) {
	return hll.New(precision)
}

func NewCountMinSketch(width, depth uint32) (*CountMinSketch, error) {
	return cms.New(width, depth)
}

func NewCountMinSketchFromError(epsilon, confidence float64) (*CountMinSketch, error) {
	return cms.FromError(epsilon, confidence)
}

func NewBloomFilter(expectedElements int, falsePositiveRate float64) (*BloomFilter, error) {
	return bloom.New(expectedElements, falsePositiveRate)
}
