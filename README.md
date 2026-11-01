# Cardinex

A probabilistic data structure for **semantic GROUP BY**: per-entity count, sum, and mean over embedding streams, where near-duplicate vectors count as one entity. Bounded memory, single pass, mergeable across workers, with a guaranteed error interval on every answer.

It also ships the classic exact-key sketches (HyperLogLog, Count-Min, Bloom) on the same binary format.

> If your vectors fit in memory, the data is static, and a global pass is fine, run FAISS k-means instead. Cardinex is for data that is distributed, continuously updated, or too large to hold.

## Layout

| Path | What |
|---|---|
| `core/semantic` | The semantic aggregation sketch (ingest, quarantine, probation, merge, top-k, lookup, diff) |
| `core/quant` | Seeded randomized-Hadamard rotation and 1/2/4-bit codes |
| `core/classic` | HyperLogLog (6-bit registers, Ertl estimator), Count-Min (optional conservative update), Bloom |
| `core/serial` | CDNX v1 binary envelope (little-endian, CRC-32C) |
| `core/build`, `core/sketch`, `core/hashx`, `core/tuner` | Constructors, typed interfaces, XXH3 hashing, parameter planning |
| `capi` | C ABI (`cardinex.h`), built with `-buildmode=c-shared` |
| `sdk/` | Go, Python, C++, C#, Java, Node.js and Rust SDKs over the C ABI |
| `service` | HTTP API and MCP server, Redis persistence |
| `conformance` | Cross-language spec and golden files |
| `bench` | Go microbenchmarks |
| `experiments` | Research harness (not yet run) |

## Go

```go
import "github.com/dippendicular/cardinex/sdk/go"

s, _ := cardinex.NewSemantic(cardinex.DefaultSemanticParams(384, 0.6, 1))
_ = s.Add(embedding, 0.7, itemID)
for _, e := range s.TopK(10, cardinex.ByMean) {
    fmt.Println(e.ID, e.Mean.Lower, e.Mean.Upper, e.Ambiguous)
}
```

## Python

```python
import numpy as np, cardinex

with cardinex.Semantic(dim=384, tau=0.6, seed=1) as s:
    s.add_batch(embeddings.astype(np.float32), values=losses)   # zero-copy
    print(s.topk(10, by="mean"))
```

## Service

```sh
docker compose up --build
```

HTTP routes:

- Classic sketches: `POST /sketches/{hll|cms|bloom}`, `POST /sketches/{id}/add`, `GET /sketches/{id}/query`.
- Semantic sketches: `POST /semantic`, `POST /semantic/{id}/add-batch`, `GET /semantic/{id}/topk`, `POST /semantic/{id}/lookup`, `POST /semantic/{id}/diff`, `GET /semantic/{id}/exemplars/{entity}`.
- Any sketch: `POST /sketches/merge`, `GET /sketches/{id}/stats`, `GET /sketches/{id}/export`, `POST /sketches/import`.

Behaviour:

- Writes are idempotent by `batch_id`.
- If you pass `tolerance`, any answer whose interval is wider than it comes back with `status: insufficient_confidence`.
- `cardinex-mcp` serves the same operations as MCP tools over stdio: `create`, `add`, `merge`, `topk`, `query`, `diff`, `exemplars`, `stats`.

Environment: `ADDR`, `REDIS_ADDR`, `FLUSH_INTERVAL` (default 5s), `LOG_WRITES` (default 1), `CARDINEX_API_KEY`.

## Building

```sh
make build        # Go
make lib          # dist/libcardinex.{so,dylib} or cardinex.dll (needs a C compiler)
make conformance  # Go + Python conformance
make cross        # all targets with zig cc
```

## Guarantees

Semantic count, sum and mean intervals are **guaranteed conditional on the η event**:

- **Separation condition:** Δ > 4ρ + 4η + 2·band must hold.
- **Values:** must lie in the declared [a, b].
- **When the conditions fail:** ambiguous observations go to a quarantine ledger and are reported next to the counts, never folded into them.

HLL intervals and the Count-Min lower bound are probabilistic. The Count-Min upper bound is guaranteed.

Stored representatives are compressed embeddings, and embeddings can be partially inverted. Sketches reduce raw-data exposure, but they are not a privacy guarantee.

## License

MIT
