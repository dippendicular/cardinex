package hll

import "testing"

func TestHLLCountMergeSerialize(t *testing.T) {
	a, err := New(10)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := New(10)
	for i := 0; i < 1000; i++ {
		a.Add([]byte{byte(i), byte(i >> 8)})
	}
	for i := 1000; i < 2000; i++ {
		b.Add([]byte{byte(i), byte(i >> 8)})
	}
	if got := a.Count(); got < 850 || got > 1150 {
		t.Fatalf("count=%d", got)
	}
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	if got := a.Count(); got < 1700 || got > 2300 {
		t.Fatalf("merged count=%d", got)
	}
	var c HyperLogLog
	if err := c.Deserialize(a.Serialize()); err != nil {
		t.Fatal(err)
	}
	if c.Count() != a.Count() {
		t.Fatal("deserialize changed count")
	}
}

func TestHLLErrors(t *testing.T) {
	if _, err := New(3); err == nil {
		t.Fatal("expected precision error")
	}
	a, _ := New(4)
	b, _ := New(5)
	if err := a.Merge(b); err == nil {
		t.Fatal("expected merge error")
	}
	if err := a.Deserialize([]byte("bad")); err == nil {
		t.Fatal("expected deserialize error")
	}
	payload := a.Serialize()
	payload[4] = 9
	if err := a.Deserialize(payload); err == nil {
		t.Fatal("expected corrupt payload error")
	}
	if _, ok := a.Query(nil).(uint64); !ok {
		t.Fatal("query should return uint64")
	}
	if a.MemoryBytes() != len(a.registers) || a.ErrorRate() <= 0 {
		t.Fatal("bad stats")
	}
	_ = alpha(16)
	_ = alpha(32)
	_ = alpha(64)
	_ = alpha(128)
	huge, _ := New(4)
	for i := range huge.registers {
		huge.registers[i] = 255
	}
	if huge.Count() == 0 {
		t.Fatal("large-range correction failed")
	}
	large, _ := New(4)
	for i := range large.registers {
		large.registers[i] = 60
	}
	if large.Count() == 0 {
		t.Fatal("large-range log correction failed")
	}
}
