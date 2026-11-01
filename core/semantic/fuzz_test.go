package semantic

import (
	"bytes"
	"os"
	"testing"
)

// FuzzUnmarshal: decoding arbitrary bytes must never panic or over-allocate,
// and a decoded sketch must round-trip byte-exactly.
func FuzzUnmarshal(f *testing.F) {
	for _, n := range []string{"semantic_d16", "semantic_d40_bits2"} {
		if b, err := os.ReadFile("../../conformance/golden/" + n + ".cdnx"); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Unmarshal(b)
		if err != nil {
			return
		}
		out, _ := s.MarshalBinary()
		s2, err := Unmarshal(out)
		if err != nil {
			t.Fatalf("re-decode: %v", err)
		}
		out2, _ := s2.MarshalBinary()
		if !bytes.Equal(out, out2) {
			t.Fatal("round trip not stable")
		}
	})
}
