# Cardinex — Claude Code Implementation Brief

Kickoff: "Read docs/IMPLEMENTATION.md and execute Phase 0. Stop at each phase's acceptance gate and report."

Background and motivation: [DESIGN.md](DESIGN.md). Model and estimators: [SPEC.md](SPEC.md), [ESTIMATORS.md](ESTIMATORS.md).

## 0. Mission

Extend the existing Cardinex repo (Go core, Chi/Redis service, HLL/CMS/Bloom) toward a library whose research contribution is the **semantic aggregation sketch**: per-entity count, sum, and mean with guaranteed intervals over embedding streams, where near-duplicate vectors denote one latent entity. Work is research-first: Phase 0 is a minimal Go prototype plus two experiments that decide the algorithm's architecture. The C ABI and multi-language SDKs come only after model v0 survives Phase 0.

Non-negotiables:

- The Go core is the only place algorithms live. SDKs never reimplement math.
- Every SDK passes the same conformance suite against the same golden files.
- No silent changes to estimator math. Anything touching docs/ESTIMATORS.md needs an explicit note in the phase report.
- Deterministic given a seed: same inputs, same seed, same bytes, on every platform.

## 1. Repo layout

```text
cardinex/
  core/                 Go module github.com/dippendicular/cardinex
    classic/            HLL, CMS, Bloom (existing, fixed per section 3)
    semantic/           semantic aggregation sketch (new, the contribution)
    quant/              seeded rotation + few-bit quantization of vectors
    hashx/              xxhash3 / wyhash wrappers
    serial/             versioned binary format, shared by all sketches
    tuner/              parameter planner (existing, extended)
  capi/                 cgo exports, cardinex.h, builds libcardinex.{so,dylib,dll}
  sdk/
    go/                 thin re-export for Go users (pure Go, no cgo)
    python/             cffi binding, wheels via cibuildwheel
    cpp/                header-only RAII wrapper over cardinex.h
    csharp/             P/Invoke, NuGet with runtimes/<rid>/native
    java/               Panama FFM (JDK 22+), JNA fallback for JDK 17
    node/               N-API via node-addon-api, prebuilt binaries
    rust/               bindgen over cardinex.h (optional, phase 5)
  service/              HTTP (Chi) + MCP server, Redis persistence
  conformance/          golden vectors, cross-language test spec
  bench/                Go microbenchmarks
  experiments/          Python scripts that produce every paper table and figure
  docs/                 SPEC.md, FORMAT.md, ESTIMATORS.md, CAPI.md
```

## 2. Go core API

Replace `Query(item) interface{}` with typed capability interfaces:

```go
type Sketch interface {
    Kind() Kind
    Merge(other Sketch) error        // returns ErrIncompatible on param mismatch
    MarshalBinary() ([]byte, error)
    UnmarshalBinary([]byte) error
}
type Cardinality interface { Sketch; AddKey([]byte); Estimate() Estimate }
type Frequency   interface { Sketch; AddKey([]byte, n uint64); Count([]byte) Estimate }
type Membership  interface { Sketch; AddKey([]byte); Contains([]byte) bool }

// Semantic aggregation sketch
type Semantic interface {
    Sketch
    Add(vec []float32, value float64, itemID uint64)
    AddBatch(vecs [][]float32, values []float64, ids []uint64)
    TopK(k int, by Order) []Entity       // Order: ByCount, BySum, ByMean
    Lookup(vec []float32) (Entity, bool)
    Diff(other *SemanticSketch) []EntityDelta
    Stats() SemanticStats                // total weight, ambiguous mass, capacity
}

type Estimate struct { Value, Lower, Upper float64 }
type Entity struct {
    ID        uint64     // stable within a sketch lineage
    Count     Estimate
    Sum       Estimate
    Mean      Estimate   // guaranteed error interval under eviction, values in [a,b]
    Ambiguous float64    // quarantined mass on this entity, never folded into Count/Sum
    Exemplars []uint64   // reservoir of item IDs, size R
}
```

All public types are safe for concurrent use. Document the locking model in docs/SPEC.md.

## 3. Fixes to the existing classic sketches

- **HLL:** remove the 2^64 large-range correction (dead with 64-bit hashes); implement HLL++ empirical bias correction; raise the precision cap to p = 18 at minimum; pack registers at 6 bits.
- **Hashing:** replace FNV with xxhash3 (128-bit); derive double-hash pairs from the two 64-bit halves.
- **CMS:** stop calling Kirsch-Mitzenmacher double hashing "pairwise independent" in docs; add an optional conservative-update mode and document that CU sketches do not merge by addition.
- **Service persistence:** stop writing full sketch bytes to Redis on every mutation. Use an in-memory sketch with periodic snapshots plus an append-only batch log for replay; configurable loss window.

## 4. Semantic sketch specification (model v0)

Model assumptions, documented verbatim in docs/SPEC.md: latent centers; valid observations within angular distance ρ of their center; centers at least Δ apart; compressed representatives within η of the observation they encode; values nonnegative and bounded in [a, b]. Anything outside the model is a violation.

Parameters (all serialized in the header):

| Param | Meaning | Default |
|---|---|---|
| `dim` | Embedding dimension d | required |
| `capacity` | Number of counters m (about 1/ε) | 4096 |
| `tau` | Assignment threshold on angular distance; set from measured ρ (τ ≥ 2ρ for recall) | required |
| `band` | Width of the observed-ambiguity band around τ | 0.05 |
| `bits` | Bits per dimension for stored representatives | 1 |
| `value_min`, `value_max` | Declared value range [a, b]; reject values outside it, reject negatives | 0, 1 |
| `exemplars` | Reservoir size R per entity | 8 |
| `probation` | Confident members needed before a counter leaves provisional state; 0 disables | 0 |
| `refine` | Representative refinement; must stay off until Phase 0 shows identity is stable | false |
| `seed` | Seed for rotation and reservoirs | required |

Three state categories, in separate structs, never mixed:

- **Aggregation state:** count, sum, inherited_count, inherited_sum.
- **Identity state:** representative code, provisional flag, confident-member count, alias links, and (when refine is on) a genuine-observation mean accumulator with its own count.
- **Uncertainty state:** a quarantine ledger of ambiguous count and sum, attached to the top-2 candidate counters.

Uncertainty state never modifies identity state. Any promotion rule needs explicit sign-off.

Update rules:

1. Normalize, rotate (seeded), quantize to `bits` per dimension.
2. Score all representatives with popcount over packed codes (linear scan is fine up to capacity 16384; keep search behind an interface).
3. **Confident match** (one candidate within τ − band, no other within τ + band): update that counter's aggregation state; add to its exemplar reservoir.
4. **Observed-ambiguous** (any other case with a candidate within τ + band): write to the quarantine ledger only. Never seed, never touch identity state.
5. **No candidate within τ + band:** Space-Saving insert, evicting the minimum-count counter. The evicted counter's count and sum move into the new counter's inherited fields. The new representative is the new item's code; identity state resets entirely.
6. **Refinement** (off in v0): the mean vector uses only genuine assigned observations (count minus inherited count), mass-weighted steps, and a per-counter drift budget.

Estimators live only in `semantic/estimators.go`, documented in docs/ESTIMATORS.md:

- **Count and sum:** guaranteed intervals [value − inherited, value]; quarantine mass reported alongside, never folded in.
- **Mean:** guaranteed interval from the count and sum intervals plus [a, b]. Name it an *error interval*, never a confidence interval. Pure function, exhaustive tests including zero own mass and all-inherited cases.
- Leave a marked block in ESTIMATORS.md where the paper's proofs will be referenced. Do not invent tighter bounds.

Merge (mergeable-summaries procedure):

1. Reject if dim, bits, seed, tau, or band differ.
2. Fuse counters only on a confident representative match. Matches inside the band become alias links, not fusions; report them in merge stats.
3. Sum aggregation fields of fused counters; union the rest; if over capacity, keep the top `capacity` by count and fold the remainder into inherited mass per the documented rule.
4. Quarantine ledgers merge by addition; exemplar reservoirs by weighted reservoir sampling.

## 5. Serialization format

Specify in docs/FORMAT.md before writing code:

- Magic `CDNX`, format version (u16), sketch kind (u16), param block, payload, CRC32C trailer.
- Little-endian throughout; floats as IEEE-754; no Go-specific encodings (no gob).
- Readers must reject unknown major versions and accept unknown minor-version trailing fields.
- Golden files for every sketch kind live in `conformance/golden/` and never change once released.

## 6. C ABI (capi/)

Build with `go build -buildmode=c-shared`. Rules:

- Opaque handles (`uint64_t`) only; never pass Go pointers to C (respect cgo pointer rules).
- Every function returns `int32_t` status; `cdnx_last_error(buf, len)` returns a thread-local message.
- Callers own input buffers; output byte buffers are returned via `cdnx_bytes_free`.
- Batch entry points (`cdnx_semantic_add_batch`) take contiguous `float*` of n×d to amortize cgo call cost (~100 ns per crossing).
- Explicit `cdnx_*_free(handle)` for every constructor.
- Document the one-Go-runtime-per-process limitation: two different Go c-shared libraries in one process can conflict.
- Cross-compile with `zig cc` for: linux amd64/arm64 (glibc and musl), darwin arm64/amd64, windows amd64.

## 7. SDKs

Each SDK: idiomatic API, wraps handles in the language's resource-safety construct, ships prebuilt native binaries, passes the conformance suite, has one runnable example and one integration example.

| Language | Binding | Packaging | Resource safety | Integration example |
|---|---|---|---|---|
| Go | Pure Go re-export of core/, no cgo | Go module | GC | Concurrent ingest with goroutines |
| Python | cffi over cardinex.h; NumPy arrays passed zero-copy | Wheels via cibuildwheel (manylinux, musllinux, macOS, Windows) | Context manager + `__del__` fallback | Hugging Face `datasets.map` + merge; Ray Data |
| C++ | Header-only wrapper, C++17 | CMake `find_package`, vcpkg port | RAII, move-only types | Batch ingest from a float buffer |
| C# | P/Invoke with SafeHandle | NuGet, `runtimes/<rid>/native` | IDisposable | ASP.NET minimal API endpoint |
| Java | Panama FFM (JDK 22+); JNA fallback for 17 | Maven Central, platform classifiers | AutoCloseable + Cleaner | Spark UDAF |
| Node.js | N-API via node-addon-api | npm with prebuilds (prebuildify) | Explicit `close()` + FinalizationRegistry | Stream pipeline over JSONL embeddings |
| Rust (phase 5) | bindgen | crates.io `cardinex-sys` + `cardinex` | Drop | Rayon parallel ingest |

Python is the priority SDK: it is what ML engineers touch. NumPy float32 C-contiguous arrays must cross without copying.

## 8. Service

- HTTP (Chi): keep existing routes; add `/semantic` routes for create, add-batch, topk, lookup, diff, merge, stats.
- MCP server exposing tools `create`, `add`, `merge`, `topk`, `query`, `diff`, `exemplars`, `stats`. Every numeric answer includes its interval and ambiguous mass. Writes are idempotent by `batch_id`.
- Return an explicit `insufficient_confidence` status when an interval is wider than a caller-supplied tolerance.

## 9. Conformance suite

- `conformance/spec.yaml` lists operations and expected outputs (exact bytes for serialization, values within tolerance for estimates).
- Each SDK has a runner that executes the spec. CI runs every runner on every platform.
- Cross-language round-trip: build in Go, merge in Python, deserialize in Java, query in Node; results must match the Go reference.

## 10. Experiments and benchmarks

### Phase 0 harness (experiments/phase0/)

- **Separation study:** load constructed-identity groups (DISC21 augmentation groups; LLM paraphrase groups from seed sentences) and reference-only labels (Quora duplicates, MTEB clustering). Output within- and between-group distance histograms with tails, recall vs false-match rate for a single global threshold, the same after quantization at 1, 2, and 4 bits, and the measured violation fraction.
- **Adversarial harness:** synthetic von Mises-Fisher entities with controlled ρ and Δ; Zipf entity sizes. Six attacks: boundary seed, drift injection, merge fusion, outlier seed (6ρ vs probation), hidden second entity, coordinated probation poisoning.
- Every attack runs against a **counterfactual control**: the same valid stream, order, capacity, and merge tree, without the injected violations. Report amplification (V + E_prop) / V from per-entity aggregate errors relative to the control, plus false merges, false splits, heavy-hitter recall, memory, and results across at least three merge-tree shapes (sequential, balanced binary, random).

### Phase 1 (experiments/, bench/)

- Go benchmarks for add, add-batch, merge, top-k, serialize at d = 384, 768, 1024 and capacity 1k–16k.
- Equal-byte comparison against sample + k-means and offline k-means, counting representatives, counters, and metadata; SimHash + Count-Min and SimHash + Space-Saving as bias baselines.
- One script per paper table or figure, writing CSV + PDF to `experiments/out/`. No number appears in the paper that a script did not produce. `make reproduce` runs everything; PVLDB requires the package at initial submission.

## 11. Phases and acceptance gates

| Phase | Scope | Gate |
|---|---|---|
| 0a | Write SPEC.md (model v0) and ESTIMATORS.md | Reviewed by me before code |
| 0b | Minimal Go prototype: semantic sketch with ingest, top-k, merge, quarantine; no C ABI, no SDKs, no service | Unit + property tests pass; merge results within bounds across three tree shapes on valid streams |
| 0c | Phase 0 harness: separation study + six attacks with controls | Report delivered; I decide the architecture (upper bound vs characterization, Plan A vs Plan B scope) before anything else is built |
| 1 | Classic-sketch fixes (section 3); typed interfaces; versioned serializer | Existing tests pass; HLL++ error within published bounds; golden files committed |
| 2 | Semantic sketch hardened per the Phase 0 decision; refinement only if approved | Interval coverage on synthetic data meets nominal; fuzzed deserializers |
| 3 | C ABI + Python SDK + conformance runner | Python passes conformance on Linux and macOS; zero-copy NumPy verified |
| 4 | Phase 1 experiments + `make reproduce` | Every planned table and figure regenerates from one command |
| 5 | C++, C#, Java, Node.js SDKs | All pass conformance on all target platforms in CI |
| 6 | Service (HTTP + MCP), Rust SDK | MCP tools return intervals and quarantine mass; idempotent writes tested |

## 12. Working rules for Claude Code

- Stop at each gate and report: what was built, test results, open questions, any deviation from this spec.
- Ask before changing estimator math, the serialization format, or the C ABI surface.
- Prefer small, reviewable commits with conventional-commit messages.
- No new dependencies in `core/` without approval; the core stays dependency-free except for hashing.
- Fuzz the deserializers (`go test -fuzz`) and the C ABI entry points.
- Never fabricate benchmark numbers in docs or READMEs; link to generated outputs instead.
