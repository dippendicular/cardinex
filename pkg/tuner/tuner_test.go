package tuner

import "testing"

func TestTune(t *testing.T) {
	got, err := Tune(1<<20, 0.01, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Comparison) != 3 || got.HyperLogLog.Precision == 0 || got.CountMin.Width == 0 || got.Bloom.Hashes == 0 {
		t.Fatalf("bad tuning result: %+v", got)
	}
	if _, err := Tune(0, .1, 1); err == nil {
		t.Fatal("expected error")
	}
	small := tuneCMS(8, 0.0001)
	if small.Depth == 0 || small.Width == 0 {
		t.Fatal("small cms tune failed")
	}
	b := tuneBloom(1, 0.0001, 1000)
	if b.MemoryBytes > 1 || b.Hashes == 0 {
		t.Fatal("budget-limited bloom tune failed")
	}
}
