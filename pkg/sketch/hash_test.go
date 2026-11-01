package sketch

import "testing"

func TestHashing(t *testing.T) {
	a := Hash64WithSeed([]byte("x"), 1)
	b := Hash64WithSeed([]byte("x"), 2)
	if a == 0 || b == 0 || a == b {
		t.Fatal("seeded hashes should be non-zero and distinct")
	}
	h1, h2 := DoubleHash([]byte("x"))
	if h1 == 0 || h2 == 0 || h1 == h2 {
		t.Fatal("double hash should produce distinct hashes")
	}
}
