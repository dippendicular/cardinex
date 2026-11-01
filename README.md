# Cardinex

Cardinex is a dependency-free Go core library for probabilistic data structures plus a Chi/Redis HTTP service.

Import the core module:

```go
import cardinex "github.com/dippendicular/cardinex/pkg"
```

Run the service:

```sh
docker compose up --build
```

## Sketches

All structures implement:

```go
type Sketch interface {
    Add(item []byte)
    Query(item []byte) interface{}
    Merge(other Sketch) error
    Serialize() []byte
    Deserialize([]byte) error
}
```

## HTTP API

- `POST /sketches/:type` creates `hll`, `cms`, or `bloom`. Pass explicit params, or `memory_bytes`, `error_rate`, and `expected_elements` for tuning.
- `POST /sketches/:id/add` with `{"item":"user-123"}` inserts.
- `GET /sketches/:id/query?item=user-123` queries.
- `POST /sketches/merge` with `{"into":"id-a","from":"id-b"}` merges compatible sketches.
- `GET /sketches/:id/stats` returns memory, error bound, estimate, and interval.
- `DELETE /sketches/:id` destroys.

Sketch bytes are written to Redis after every mutation and loaded into memory on service startup.

## Mathematics

### HyperLogLog

HyperLogLog hashes each item to 64 bits, uses the first `p` bits as a register index, and stores the maximum leading-zero rank seen in that register. With `m = 2^p` registers, the raw estimate is:

```text
E = alpha_m * m^2 / sum(2^-M[j])
```

The harmonic mean damps the effect of outlier registers. Cardinex applies the standard small-range linear-counting correction when `E <= 5m/2` and empty registers remain:

```text
E* = m * ln(m / V)
```

For very large estimates it applies:

```text
E* = -2^64 * ln(1 - E / 2^64)
```

The relative standard error is approximately:

```text
1.04 / sqrt(m)
```

Union is register-wise maximum, making distributed cardinality sketches mergeable without raw events.

### Count-Min Sketch

Count-Min Sketch has width `w` and depth `d`. Cardinex uses double hashing to generate a pairwise-independent family:

```text
h_i(x) = h1(x) + i*h2(x)
```

Each add increments one counter per row. Query returns the minimum counter, so estimates never undercount. With:

```text
w = ceil(e / epsilon)
d = ceil(ln(1 / delta))
```

the estimate is within additive error `epsilon * total_count` with confidence `1 - delta`. Equivalently, the implemented defaults report:

```text
epsilon = e / w
confidence = 1 - (1/e)^d
```

Merge is cell-wise addition for sketches with equal width and depth.

### Bloom Filter

For expected insertions `n` and desired false-positive probability `p`, the optimal bit array size is:

```text
m = -n * ln(p) / ln(2)^2
```

and the optimal number of hash probes is:

```text
k = (m / n) * ln(2)
```

Cardinex uses FNV-based double hashing. Inserted items are guaranteed to have zero false negatives because all `k` bits are set on insert and membership only fails when one of those bits is absent. False positives occur when other items happen to set the same bits:

```text
p ~= (1 - exp(-k*n/m))^k
```

Merge is bitwise OR for equal `m` and `k`.

## Tuning Example: Unique Visitors Under 1 MB

For high-traffic unique visitor counting, HyperLogLog is the natural fit. A 1 MB budget gives `m = 1,048,576` one-byte registers (`p = 20` would exceed the current implementation cap, so Cardinex selects `p = 18`, or 262,144 bytes). The expected relative error is:

```text
1.04 / sqrt(262144) = 0.00203
```

That is about `0.2%` relative error while using only 256 KB, leaving the rest of the budget for more sketches, replication overhead, or Redis key metadata.

Use the tuner:

```go
plan, _ := tuner.Tune(1<<20, 0.01, 10_000_000)
fmt.Println(plan.Comparison)
```

## Benchmark Output

Run:

```sh
go test ./bench -run TestMarkdownBenchmarkTable -v
go test ./bench -bench . -benchmem
```

The table test prints markdown rows with memory per element and actual versus theoretical error.
