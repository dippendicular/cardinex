package cms

import "testing"

func TestCMSEstimateMergeSerialize(t *testing.T) {
	c, err := New(128, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		c.Add([]byte("a"))
	}
	c.Add([]byte("b"))
	if got := c.Estimate([]byte("a")); got < 10 {
		t.Fatalf("estimate=%d", got)
	}
	d, _ := New(128, 5)
	d.AddN([]byte("a"), 5)
	if err := c.Merge(d); err != nil {
		t.Fatal(err)
	}
	if got := c.Estimate([]byte("a")); got < 15 {
		t.Fatalf("merged estimate=%d", got)
	}
	var e CountMinSketch
	if err := e.Deserialize(c.Serialize()); err != nil {
		t.Fatal(err)
	}
	if e.Total() != c.Total() {
		t.Fatal("deserialize changed total")
	}
}

func TestCMSErrors(t *testing.T) {
	if _, err := New(0, 1); err == nil {
		t.Fatal("expected params error")
	}
	if _, err := FromError(0, .9); err == nil {
		t.Fatal("expected error")
	}
	if made, err := FromError(0.01, .99); err != nil || made.Width == 0 || made.Depth == 0 {
		t.Fatalf("unexpected FromError result: %+v %v", made, err)
	}
	a, _ := New(2, 2)
	b, _ := New(3, 2)
	if err := a.Merge(b); err == nil {
		t.Fatal("expected merge error")
	}
	if err := a.Deserialize([]byte("bad")); err == nil {
		t.Fatal("expected deserialize error")
	}
	payload := a.Serialize()
	payload[4], payload[5], payload[6], payload[7] = 0, 0, 0, 0
	if err := a.Deserialize(payload); err == nil {
		t.Fatal("expected corrupt payload error")
	}
	if _, ok := a.Query([]byte("x")).(uint64); !ok {
		t.Fatal("query should return uint64")
	}
	if a.MemoryBytes() == 0 || a.Epsilon() == 0 || a.Confidence() == 0 {
		t.Fatal("bad stats")
	}
}
