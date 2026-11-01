package pkg

import "testing"

func TestFacadeConstructors(t *testing.T) {
	if _, err := NewHyperLogLog(10); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCountMinSketch(10, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCountMinSketchFromError(.01, .99); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBloomFilter(100, .01); err != nil {
		t.Fatal(err)
	}
}
