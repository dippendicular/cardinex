# Cardinex

Semantic GROUP BY sketches over embedding streams: per-entity count, sum, and mean with guaranteed error intervals, bounded memory, one pass, mergeable. The classic HLL/CMS/Bloom sketches are exact-key building blocks, not the research contribution.

Module: `github.com/dippendicular/cardinex` (GitHub user `dippendicular`).

## Read first

- [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md) — the implementation brief: layout, API, phases, acceptance gates. This is the source of truth for what to build.
- [docs/SPEC.md](docs/SPEC.md) — model v0 and the semantic sketch algorithm.
- [docs/ESTIMATORS.md](docs/ESTIMATORS.md) — every interval formula and the argument for it.
- [docs/DESIGN.md](docs/DESIGN.md) — motivation, related work, research questions, evaluation plan.

## Current phase

- 0a (SPEC.md + ESTIMATORS.md): approved 2026-11-02. SPEC §11 lists what it supersedes in the brief.
- 0b (Go prototype: `quant/`, `semantic/`): built; **awaiting gate review**. Do not start 0c until it is signed off.

## Working rules

- Stop at each phase gate and report: what was built, test results, open questions, deviations from the brief.
- Ask before changing estimator math, the serialization format, or the C ABI surface. Estimator math lives only in `semantic/estimators.go` and must match docs/ESTIMATORS.md.
- The core is dependency-free except for hashing. No new dependencies in core without approval.
- Deterministic given a seed on every platform: no map iteration in output order, explicit tie-breaks, wrap float products in `float64(...)` where Go could fuse them into FMA (arm64).
- Never call a guaranteed error interval a "confidence interval". Never invent tighter bounds than the ones derived in ESTIMATORS.md.
- Fuzz every deserializer. Never fabricate benchmark numbers; link to generated outputs.
- Small commits, conventional-commit messages.

## Commands

```sh
go build ./...
go vet ./...
go test -race ./...
go test -bench . ./bench/
```
