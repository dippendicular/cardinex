package bloom

import "testing"

func TestBloomContainsMergeSerialize(t *testing.T) {
	b, err := New(1000, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	b.Add([]byte("x"))
	if !b.Contains([]byte("x")) {
		t.Fatal("false negative")
	}
	o, _ := NewWithParams(b.Bits, b.K)
	o.Add([]byte("y"))
	if err := b.Merge(o); err != nil {
		t.Fatal(err)
	}
	if !b.Contains([]byte("y")) {
		t.Fatal("merged item missing")
	}
	var c BloomFilter
	if err := c.Deserialize(b.Serialize()); err != nil {
		t.Fatal(err)
	}
	if !c.Contains([]byte("x")) || !c.Contains([]byte("y")) {
		t.Fatal("deserialize changed membership")
	}
}

func TestBloomErrors(t *testing.T) {
	if _, err := New(0, .1); err == nil {
		t.Fatal("expected params error")
	}
	if _, err := New(1, 1); err == nil {
		t.Fatal("expected fpr error")
	}
	if _, err := NewWithParams(0, 1); err == nil {
		t.Fatal("expected params error")
	}
	a, _ := NewWithParams(8, 1)
	b, _ := NewWithParams(16, 1)
	if err := a.Merge(b); err == nil {
		t.Fatal("expected merge error")
	}
	if err := a.Deserialize([]byte("bad")); err == nil {
		t.Fatal("expected deserialize error")
	}
	payload := a.Serialize()
	payload[4], payload[5], payload[6], payload[7], payload[8], payload[9], payload[10], payload[11] = 0, 0, 0, 0, 0, 0, 0, 0
	if err := a.Deserialize(payload); err == nil {
		t.Fatal("expected corrupt payload error")
	}
	if _, ok := a.Query([]byte("x")).(bool); !ok {
		t.Fatal("query should return bool")
	}
	if a.Contains([]byte("missing")) {
		t.Fatal("tiny empty-ish filter should miss")
	}
	if a.MemoryBytes() == 0 || a.Inserts() != 0 || a.FalsePositiveRate() < 0 {
		t.Fatal("bad stats")
	}
}
