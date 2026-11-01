// Command gen writes conformance/spec.json and conformance/golden/*.cdnx.
// Run from the repo root: go run ./conformance/gen
// Golden files never change once released; regenerate only before v1.0.
package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/dippendicular/cardinex/conformance"
	"github.com/dippendicular/cardinex/core/build"
)

func main() {
	spec := conformance.Spec{Version: 1}
	for _, c := range conformance.DefaultCases {
		sk, err := conformance.Run(c, build.New)
		if err != nil {
			log.Fatalf("%s: %v", c.Name, err)
		}
		b, err := sk.MarshalBinary()
		if err != nil {
			log.Fatal(err)
		}
		c.Golden = "golden/" + c.Name + ".cdnx"
		if err := os.WriteFile(filepath.Join("conformance", c.Golden), b, 0o644); err != nil {
			log.Fatal(err)
		}
		c.Expect = conformance.Answers(c, sk)
		spec.Cases = append(spec.Cases, c)
	}
	out, _ := json.MarshalIndent(spec, "", "  ")
	if err := os.WriteFile("conformance/spec.json", append(out, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
