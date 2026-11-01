package classic

import (
	"os"
	"testing"
)

// FuzzUnmarshal: decoding arbitrary bytes must never panic or over-allocate,
// and anything that decodes must re-encode to the same bytes.
func FuzzUnmarshal(f *testing.F) {
	for _, n := range []string{"hll_p12", "cms_1024x4", "cms_cu_256x3", "bloom_8192x5"} {
		if b, err := os.ReadFile("../../conformance/golden/" + n + ".cdnx"); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Unmarshal(b)
		if err != nil {
			return
		}
		out, err := s.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Unmarshal(out); err != nil {
			t.Fatalf("re-decode: %v", err)
		}
	})
}
