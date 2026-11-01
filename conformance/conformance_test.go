package conformance_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/dippendicular/cardinex/conformance"
	"github.com/dippendicular/cardinex/core/build"
)

// TestGo is the Go reference runner of conformance/spec.json.
func TestGo(t *testing.T) {
	raw, err := os.ReadFile("spec.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec conformance.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	for _, c := range spec.Cases {
		t.Run(c.Name, func(t *testing.T) {
			golden, err := os.ReadFile(c.Golden)
			if err != nil {
				t.Fatal(err)
			}
			sk, err := conformance.Run(c, build.New)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := sk.MarshalBinary()
			if !bytes.Equal(got, golden) {
				t.Fatalf("bytes differ from %s", c.Golden)
			}
			dec, err := build.Decode(golden)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(conformance.Answers(c, dec))
			b, _ := json.Marshal(c.Expect)
			if !jsonClose(a, b) {
				t.Fatalf("answers differ:\n got %s\nwant %s", a, b)
			}
		})
	}
}

func jsonClose(a, b []byte) bool {
	var x, y any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	return close(x, y)
}

func close(x, y any) bool {
	switch xv := x.(type) {
	case float64:
		yv, ok := y.(float64)
		return ok && math.Abs(xv-yv) <= 1e-9
	case []any:
		yv, ok := y.([]any)
		if !ok || len(xv) != len(yv) {
			return false
		}
		for i := range xv {
			if !close(xv[i], yv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		yv, ok := y.(map[string]any)
		if !ok || len(xv) != len(yv) {
			return false
		}
		for k := range xv {
			if !close(xv[k], yv[k]) {
				return false
			}
		}
		return true
	}
	return x == y
}
